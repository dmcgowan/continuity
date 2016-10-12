package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"

	"github.com/Sirupsen/logrus"
	"github.com/docker/distribution/digest"
	"github.com/mitchellh/cli"
	"github.com/stevvooe/continuity"
	"github.com/stevvooe/continuity/continuityfs"
)

type mountCommand struct {
	lowerDir  string
	upperDir  string
	workDir   string
	mountType string
	fstab     bool

	flagSet *flag.FlagSet
}

func mountCommandFactory() (cli.Command, error) {
	mc := &mountCommand{
		flagSet: flag.NewFlagSet("", flag.ContinueOnError),
	}

	mc.flagSet.SetOutput(ioutil.Discard)
	mc.flagSet.StringVar(&mc.workDir, "w", "", "Work directory for overlay")
	mc.flagSet.StringVar(&mc.upperDir, "u", "", "Upper directory for overlay")
	mc.flagSet.StringVar(&mc.lowerDir, "l", "", "Lower directory for overlay, read only manifest location")
	mc.flagSet.StringVar(&mc.mountType, "t", "checkout", "Read-only manifest mount type")
	mc.flagSet.BoolVar(&mc.fstab, "fstab", false, "Read mount information from fstab")
	// remount flag, use existing overlay upper
	// preserve, do not cleanup overlay upper directory after commit

	return mc, nil
}

func (cmd *mountCommand) Help() string {
	buf := bytes.NewBuffer(nil)
	fmt.Fprintf(buf, "usage:  %s mount [options] <location> <continuity manifest hash>\n\n", os.Args[0])
	fmt.Fprintln(buf, "Mount Options:")
	cmd.flagSet.SetOutput(buf)
	defer cmd.flagSet.SetOutput(ioutil.Discard)
	cmd.flagSet.PrintDefaults()
	return buf.String()
}

func (cmd *mountCommand) Synopsis() string {
	return "Mount a continuity filesystem to a directory"
}

func (cmd *mountCommand) Run(args []string) int {
	if err := cmd.flagSet.Parse(args); err != nil {
		if err != flag.ErrHelp {
			logrus.Errorf("Argument error: %v", err)
		}
		return cli.RunResultHelp
	}

	args = cmd.flagSet.Args()

	if len(args) != 2 {
		return cli.RunResultHelp

	}
	mountpoint, err := filepath.Abs(args[0])
	if err != nil {
		logrus.Errorf("Bad mountpoint %q: %v", args[0], err)
		return 1
	}

	dgst, err := digest.ParseDigest(args[1])
	if err != nil {
		logrus.Printf("error parsing digest: %v", err)
		return 1
	}

	mountStat, err := os.Stat(mountpoint)
	if err != nil {
		logrus.Errorf("Error statting mount directory: %v", err)
	}
	mpSt := mountStat.Sys().(*syscall.Stat_t)
	uid := int(mpSt.Uid)
	gid := int(mpSt.Gid)

	names := []string{"lower", "upper", "work"}
	dirs := []*string{&cmd.lowerDir, &cmd.upperDir, &cmd.workDir}
	for i, s := range dirs {
		if *s == "" {
			td, err := ioutil.TempDir(mountDir, names[i]+"-")
			if err != nil {
				logrus.Errorf("error creating %s directory: %v", names[i], err)
				return 1
			}
			defer os.RemoveAll(td)
			if err := os.Chown(td, uid, gid); err != nil {
				logrus.Warnf("Error chowning mountpoint: %v", err)
			}
			*s = td
		}
	}

	f, err := blobStore.Open("", dgst)
	if err != nil {
		logrus.Printf("error opening digest %s: %v", dgst, err)
		return 1
	}

	manifestBytes, err := ioutil.ReadAll(f)
	if err != nil {
		logrus.Printf("error reading manifest: %v", err)
		return 1
	}

	manifest, err := continuity.Unmarshal(manifestBytes)
	if err != nil {
		logrus.Errorf("error unmarshalling manifest: %v", err)
		return 1
	}

	logrus.Debugf("Mounting %s to %s", dgst, cmd.lowerDir)

	var mounter Mounter
	switch cmd.mountType {
	case "checkout":
		mounter, err = newCheckoutMounter(cmd.lowerDir, manifest, blobStore)
	case "fuse":
		mounter, err = newFuseMounter(cmd.lowerDir, manifest, blobStore)
	default:
		logrus.Errorf("Unsupported mount type: %s", cmd.mountType)
		return 1
	}
	if err != nil {
		logrus.Errorf("Error initializing mount: %v", err)
		return 1
	}

	if err := mounter.Mount(); err != nil {
		logrus.Errorf("Error mounting manifest: %v", err)
		return 1
	}

	var mountArgs []string
	if cmd.fstab {
		mountArgs = []string{mountpoint}
	} else {
		mountArgs = []string{
			"-t", "overlay", "overlay",
			fmt.Sprintf("-olowerdir=%s,upperdir=%s,workdir=%s", cmd.lowerDir, cmd.upperDir, cmd.workDir),
			mountpoint,
		}
	}
	if err := exec.Command("mount", mountArgs...).Run(); err != nil {
		logrus.Errorf("Error mounting overlay: %v", err)
		if err := mounter.Unmount(); err != nil {
			logrus.Errorf("Error unmounting manifest: %v", err)
		}
		return 2
	}

	if err := os.Chown(mountpoint, uid, gid); err != nil {
		logrus.Warnf("Error chowning mountpoint: %v", err)
	}

	logrus.Infof("Mounted %s", mountpoint)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	signal.Notify(sigChan, syscall.SIGTERM)

	<-sigChan

	go func() {
		<-sigChan
		os.Exit(1)
	}()

	logrus.Infof("Unmounting")

	if err := exec.Command("umount", mountpoint).Run(); err != nil {
		logrus.Errorf("Error unmounting overlay: %v", err)
		return 2
	}

	if err := mounter.Unmount(); err != nil {
		logrus.Errorf("Error unmounting manifest: %v", err)
		return 1
	}

	logrus.Infof("Committing")

	options := continuity.ContextOptions{
		Digester: blobStore,
		// TODO: Add whiteout handler
	}

	ctx, err := continuity.NewContextWithOptions(cmd.upperDir, options)
	if err != nil {
		logrus.Printf("error creating path context: %v", err)
		return 1
	}

	diff, err := continuity.BuildManifest(ctx)
	if err != nil {
		logrus.Printf("error generating manifest: %v", err)
		return 1
	}

	m := continuity.MergeManifests(manifest, diff)

	// Add merged manifest to blobstore and output hash
	p, err := continuity.Marshal(m)
	if err != nil {
		logrus.Printf("error marshaling manifest: %v", err)
		return 1
	}

	newDgst, err := blobStore.Digest(bytes.NewReader(p))
	if err != nil {
		logrus.Printf("error getting digest of manifest: %v", err)
		return 1
	}

	fmt.Fprintf(os.Stdout, "%s\n", newDgst.String())

	return 0
}

type overlayWhiteoutChecker struct{}

func (overlayWhiteoutChecker) IsOpaque(r continuity.Resource) bool {
	if d, ok := r.(continuity.Device); ok {
		return d.Major() == 0 && d.Minor() == 0
	}
	return false
}

func (overlayWhiteoutChecker) IsWhiteout(r continuity.Resource) bool {
	if d, ok := r.(continuity.Directory); ok {
		xattrs := d.XAttrs()
		if o, ok := xattrs["overlay.opaque"]; ok {
			return len(o) == 1 && o[0] == 'y'
		}
	}
	return false
}

type Mounter interface {
	Mount() error
	Unmount() error
}

type checkoutMounter struct {
	context  continuity.Context
	manifest *continuity.Manifest
}

func (cm *checkoutMounter) Mount() error {
	return continuity.ApplyManifest(cm.context, cm.manifest)
}

func (cm *checkoutMounter) Unmount() error {
	return nil
}

func newCheckoutMounter(root string, manifest *continuity.Manifest, provider continuity.ContentProvider) (Mounter, error) {
	options := continuity.ContextOptions{
		Provider: provider,
	}
	context, err := continuity.NewContextWithOptions(root, options)
	if err != nil {
		return nil, err
	}
	return &checkoutMounter{
		context:  context,
		manifest: manifest,
	}, nil
}

type fuseMounter struct {
	root string
	fs   fs.FS
	conn *fuse.Conn

	errL sync.Mutex
	err  error
}

func (fm *fuseMounter) Mount() error {
	c, err := fuse.Mount(
		fm.root,
		fuse.ReadOnly(),
		fuse.FSName("manifest"),
		fuse.Subtype("continuity"),
		// OSX Only options
		fuse.LocalVolume(),
		fuse.VolumeName("Continuity FileSystem"),
	)
	if err != nil {
		return err
	}

	<-c.Ready
	if err := c.MountError; err != nil {
		c.Close()
		return err
	}

	go func() {
		// TODO: Create server directory to use context
		err = fs.Serve(c, fm.fs)
		if err != nil {
			logrus.Errorf("Server error: %v", err)
			fm.errL.Lock()
			fm.err = err
			fm.errL.Unlock()
		}
	}()
	fm.conn = c

	return nil
}

func (fm *fuseMounter) Unmount() error {
	if fm.conn == nil {
		return nil
	}
	c := fm.conn
	fm.conn = nil

	closeC := make(chan error)
	go func() {
		if err := c.Close(); err != nil {
			closeC <- err
		}
		close(closeC)
	}()

	var closeErr error
	timeoutC := time.After(time.Second)

	select {
	case <-timeoutC:
		closeErr = errors.New("close timed out")
	case closeErr = <-closeC:
	}

	if closeErr != nil {
		logrus.Errorf("Unable to close connection: %v", closeErr)
	}

	if err := fuse.Unmount(fm.root); err != nil {
		logrus.Errorf("Error unmounting %s: %v", fm.root, err)
		return err
	}

	fm.errL.Lock()
	defer fm.errL.Unlock()
	return fm.err
}

func newFuseMounter(root string, manifest *continuity.Manifest, provider continuityfs.FileContentProvider) (Mounter, error) {
	manifestFS, err := continuityfs.NewFSFromManifest(manifest, root, provider)
	if err != nil {
		return nil, err
	}
	return &fuseMounter{
		root: root,
		fs:   manifestFS,
	}, nil
}
