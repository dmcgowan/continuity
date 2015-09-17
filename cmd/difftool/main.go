package main

import (
	"flag"
	"fmt"
	"sort"

	"github.com/Sirupsen/logrus"
	"github.com/stevvooe/continuity"
	"github.com/stevvooe/continuity/diffutil"
)

func main() {
	debug := flag.Bool("d", false, "Debug mode")
	flag.Parse()
	if flag.NArg() != 2 {
		flag.Usage()
		logrus.Fatalf("Required passing 2 directories")
	}
	if *debug {
		logrus.SetLevel(logrus.DebugLevel)
	}

	c1, err := continuity.NewContext(flag.Arg(0))
	if err != nil {
		logrus.Fatalf("Error getting context for path %s", flag.Arg(0))
	}
	c2, err := continuity.NewContext(flag.Arg(1))
	if err != nil {
		logrus.Fatalf("Error getting context for path %s", flag.Arg(1))
	}
	m1, err := continuity.BuildManifest(c1)
	if err != nil {
		logrus.Fatalf("Error getting manifest for path %s", flag.Arg(0))
	}
	m2, err := continuity.BuildManifest(c2)
	if err != nil {
		logrus.Fatalf("Error getting manifest for path %s", flag.Arg(1))
	}

	diff := diffutil.DiffManifest(m1, m2)

	displayDiff(diff)
}

func displayDiff(diffutil.DiffManifest) {
	changeLines := map[string]string{}
	files := []string{}

	for i := range diff.Additions {
		p := displayPath(diff.Additions[i])
		files = append(files, p)
		changeLines[p] = fmt.Sprintf("\033[32m++ %s\033[0m", p)
	}

	for i := range diff.Deletions {
		p := displayPath(diff.Deletions[i])
		files = append(files, p)
		changeLines[p] = fmt.Sprintf("\033[31m-- %s\033[0m", p)
	}

	for i := range diff.Updates {
		p := displayPath(diff.Updates[i].Original)
		files = append(files, p)
		changeLines[p] = fmt.Sprintf("\033[33m<> %s\033[0m", p)
	}

	sort.Strings(files)
	for i := range files {
		fmt.Printf("%s\n", changeLines[files[i]])
	}

	fmt.Printf("additions: %d deletions: %d updates: %d\n", len(diff.Additions), len(diff.Deletions), len(diff.Updates))
}

func displayPath(r continuity.Resource) string {
	if d, ok := r.(continuity.Directory); ok {
		return d.Path() + "/"
	}
	return r.Path()
}
