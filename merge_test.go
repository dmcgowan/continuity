package continuity

import (
	"os"
	"sort"
	"testing"

	"github.com/docker/distribution/digest"
)

func randomDigest(size int) digest.Digest {
	d := make([]byte, size)
	randomBytes(d)
	return digest.FromBytes(d)
}

func TestMergeOverlay(t *testing.T) {
	layer1 := []dresource{
		{
			kind: rdirectory,
			path: "a",
			mode: 0755,
		},
		{
			kind:   rfile,
			size:   4085,
			digest: randomDigest(4085),
			path:   "a/f1",
			mode:   0600,
		},
		{
			kind:   rfile,
			size:   1023,
			digest: randomDigest(1023),
			path:   "a/f2",
			mode:   0600,
		},
		{
			kind: rdirectory,
			path: "b",
			mode: 0755,
		},
		{
			kind:   rfile,
			size:   1023,
			digest: randomDigest(1023),
			path:   "b/hidden",
			mode:   0600,
		},
		{
			kind: rdirectory,
			path: "c",
			mode: 0755,
		},
		{
			path: "c/f1",
			mode: 0600,
		},
	}
	layer2 := []dresource{
		{
			kind: rdirectory,
			path: "a",
			mode: 0755,
		},
		{
			kind:   rfile,
			size:   1022,
			digest: randomDigest(1022),
			path:   "a/f2",
			mode:   0644,
		},
		{
			kind:   rfile,
			size:   234,
			digest: randomDigest(234),
			path:   "a/f3",
			mode:   0600,
		},
		{
			kind: rdirectory,
			path: "b",
			mode: 0755,
			xattrs: map[string][]byte{
				"trusted.overlay.opaque": []byte{'y'},
			},
		},
		{
			kind:   rfile,
			size:   1023,
			digest: randomDigest(1023),
			path:   "b/nothidden",
			mode:   0600,
		},
		{
			kind:  rchardev,
			path:  "c",
			mode:  0755,
			major: 0,
			minor: 0,
		},
	}
	result := []dresource{
		layer2[0],
		layer1[1],
		layer2[1],
		layer2[2],
		{
			kind: rdirectory,
			path: "b",
			mode: 0755,
		},
		layer2[4],
	}

	checkMerge(t, layer1, layer2, result, MergeOverlay)
}

func TestMergeAUFS(t *testing.T) {
	// TODO: test with hardlinks
	// TODO: test with file named ".aa"
	// TODO: test with whiteout sub-directories
	// TODO: test with opaque sub-directories
	layer1 := []dresource{
		{
			kind: rdirectory,
			path: "a",
			mode: 0755,
		},
		{
			kind:   rfile,
			size:   4085,
			digest: randomDigest(4085),
			path:   "a/f1",
			mode:   0600,
		},
		{
			kind:   rfile,
			size:   1023,
			digest: randomDigest(1023),
			path:   "a/f2",
			mode:   0600,
		},
		{
			kind: rdirectory,
			path: "b",
			mode: 0755,
		},
		{
			kind:   rfile,
			size:   1023,
			digest: randomDigest(1023),
			path:   "b/hidden",
			mode:   0600,
		},
		{
			kind: rdirectory,
			path: "c",
			mode: 0755,
		},
		{
			path: "c/f1",
			mode: 0600,
		},
	}
	layer2 := []dresource{
		{
			kind: rdirectory,
			path: "a",
			mode: 0755,
		},
		{
			kind:   rfile,
			size:   1022,
			digest: randomDigest(1022),
			path:   "a/f2",
			mode:   0644,
		},
		{
			kind:   rfile,
			size:   234,
			digest: randomDigest(234),
			path:   "a/f3",
			mode:   0600,
		},
		{
			kind: rdirectory,
			path: "b",
			mode: 0755,
		},
		{
			kind: rfile,
			path: "b/.wh..wh..opq",
			mode: 0755,
		},
		{
			kind:   rfile,
			size:   1023,
			digest: randomDigest(1023),
			path:   "b/nothidden",
			mode:   0600,
		},
		{
			kind: rfile,
			path: ".wh.c",
			size: 0,
		},
	}
	result := []dresource{
		layer2[0],
		layer1[1],
		layer2[1],
		layer2[2],
		layer2[3],
		layer2[5],
	}

	checkMerge(t, layer1, layer2, result, MergeAUFS)
}

func checkMerge(t *testing.T, layer1, layer2, result []dresource, merge func(*Manifest, *Manifest) *Manifest) {
	r1, err := expectedResourceList(layer1)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := expectedResourceList(layer2)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := expectedResourceList(result)
	if err != nil {
		t.Fatal(err)
	}

	mm := merge(&Manifest{Resources: r1}, &Manifest{Resources: r2})

	diff := diffResourceList(expected, mm.Resources)
	if diff.HasDiff() {
		t.Log("Resource list difference")
		for _, a := range diff.Additions {
			t.Logf("Unexpected resource: %#v", a)
		}
		for _, d := range diff.Deletions {
			t.Logf("Missing resource: %#v", d)
		}
		for _, u := range diff.Updates {
			t.Logf("Changed resource:\n\tExpected: %#v\n\tActual:   %#v", u.Original, u.Updated)
		}

		t.FailNow()
	}
}

func TestAUFSSort(t *testing.T) {
	unsorted := []string{
		"a",
		".hidden/",
		".hidden/fun",
		".hidden/.anotherhidden",
		".hidden/.wh..shh",
		".hidden/.wh.nowdeleted",
		".hidden/sub/",
		".hidden/sub/only-me",
		".hidden/sub/.wh..wh..opq",
		"AUTHORS",
		".wh.README.md",
		".aaaaaaahhhhhh",
	}
	expected := []string{
		".wh.README.md",
		".aaaaaaahhhhhh",
		".hidden/",
		".hidden/.wh..shh",
		".hidden/.wh.nowdeleted",
		".hidden/.anotherhidden",
		".hidden/fun",
		".hidden/sub/",
		".hidden/sub/.wh..wh..opq",
		".hidden/sub/only-me",
		"AUTHORS",
		"a",
	}

	sorted := sortAsResourcePaths(unsorted)

	if len(sorted) != len(expected) {
		t.Fatalf("Unexpected size difference: %d, expected %d", len(sorted), len(expected))
	}

	for i := range expected {
		if sorted[i] != expected[i] {
			t.Errorf("Unexpected value %d: %q, expected %q", i+1, sorted[i], expected[i])
		}
	}
}

type pathResource string

func (p pathResource) Path() string      { return string(p) }
func (p pathResource) Mode() os.FileMode { return 0644 }
func (p pathResource) UID() string       { return "" }
func (p pathResource) GID() string       { return "" }

func sortAsResourcePaths(paths []string) []string {
	resources := make([]Resource, len(paths))
	for i, s := range paths {
		resources[i] = pathResource(s)
	}
	sort.Sort(byAUFSPath(resources))
	sortedPaths := make([]string, len(resources))
	for i, r := range resources {
		sortedPaths[i] = r.Path()
	}
	return sortedPaths
}
