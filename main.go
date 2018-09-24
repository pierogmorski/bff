// A really simple big file/directory finder.

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
)

// A FileRec wraps os.FileInfo information for a file.  Path and Size are provided as os.FileInfo.Name() provides
// only the base name, and os.FileInfo.Size() does not take into account directory contents.
type FileRec struct {
	Path     string        // The full path of a file.
	Size     int64         // Size of the file.  If file is a directory, it's the sum of the sizes of it's contents.
	FileInfo os.FileInfo   // Interface describing the file.
	Contents []os.FileInfo // Slice containing directory contents.
}

// Implement sort.Interface (Len, Swap and Less), as  we want to sort our collection of FileRec entries by their size.
type bySize []*FileRec

func (bs bySize) Len() int {
	return len(bs)
}

func (bs bySize) Swap(i, j int) {
	bs[i], bs[j] = bs[j], bs[i]
}

// Less is actually reversed, as we want to sort from largest to smallest FileRec's.
func (bs bySize) Less(i, j int) bool {
	return bs[i].Size > bs[j].Size
}

// Implement Stringer interface.
func (b FileRec) String() string {
	return fmt.Sprintf("size: %v bytes -> %v", b.Size, b.Path)
}

// NewFileRec produces a ready-to-use FileRec pointer, including a full Path and Size.  If the FileRec represents
// a directory, Size will be the sum of the sizes of the directory contents, and Contents will be a slice of
// os.FileInfo structs representing the directory contents.  In the case of any errors, NewFileRec will return a
// zero-value FileRec pointer and a non-nil error describing the failure.
func NewFileRec(p string) (*FileRec, error) {
	f := &FileRec{}

	absPath, err := filepath.Abs(p)
	if err != nil {
		return f, err
	}

	// Ensure p exists.  Don't follow symlinks.
	pFileInfo, err := os.Lstat(absPath)
	if err != nil {
		return f, err
	}

	// If the path p reprents a directory, store the directory contents and sum the sizes of the contents.
	if pFileInfo.IsDir() {
		dir, err := os.Open(absPath)
		defer dir.Close()
		if err != nil {
			return f, err
		}

		dirContents, err := dir.Readdir(0)
		if err != nil {
			return f, err
		}

		size := int64(0)
		for _, dirEntry := range dirContents {
			size += dirEntry.Size()
		}

		f.Contents = dirContents
		f.Size = size
	} else {
		f.Size = pFileInfo.Size()
	}

	f.Path = absPath
	f.FileInfo = pFileInfo

	return f, nil
}

// InsertSorted appends a FileRec pointer to a slice, and returns a trimmed slice up to max elements.
func InsertSorted(frSlice []*FileRec, fr *FileRec, max int) []*FileRec {
	frSlice = append(frSlice, fr)
	sort.Sort(bySize(frSlice))
	if len(frSlice) < max {
		max = len(frSlice)
	}
	return frSlice[:max]
}

// Walk recursively walks paths, starting at basePath, and pumps FileRec pointers into the FileRec pointer channel.
func Walk(fi os.FileInfo, basePath string, fileRecCh chan *FileRec) {
	fr, err := NewFileRec(basePath + "/" + fi.Name())
	if err != nil {
		log.Printf("failed to create FileRec: %v, skipping", err)
		return
	} else {
		fileRecCh <- fr
	}

	// If fr is a directory itself, recursively walk it.
	if fr.FileInfo.IsDir() {
		for _, e := range fr.Contents {
			Walk(e, fr.Path, fileRecCh)
		}
	}
}

// GoWalk is a wrapper around Walk.  It's spooled up as a go routine and signals when it's done.
func GoWalk(fi os.FileInfo, basePath string, fileRecCh chan *FileRec, doneCh chan int) {
	Walk(fi, basePath, fileRecCh)
	doneCh <- 1
}

func main() {
	// Override default flag usage message.
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] directory\n", os.Args[0])
		flag.PrintDefaults()
	}

	// Limit results option.  Defaults to 10.
	resultLimit := flag.Int("limit", 10, "limit number of results to display")
	flag.Parse()

	// We only care about the first positional argument as we'll only process one path at a time.
	if flag.NArg() < 1 {
		log.Fatal("directory path not provided")
	}
	pathStr := flag.Arg(0)

	// The starting point of our search must be a directory.
	rootFileRec, err := NewFileRec(pathStr)
	if err != nil {
		log.Fatalf("failure in %v: %v", pathStr, err)
	}
	if !rootFileRec.FileInfo.IsDir() {
		log.Fatalf("%v is not a directory", rootFileRec.Path)
	}

	// Start our slices off with the root search path.
	bigFiles := []*FileRec{}
	bigDirs := []*FileRec{rootFileRec}

	fileRecCh := make(chan *FileRec) // Receives FileRec pointers from GoWalk go routines.
	doneCh := make(chan int)         // Receives notification that a given go routine has finished walking it's path.

	// Traverse contents of rootFileRec and spool up a go routine to walk each entry.
	for _, e := range rootFileRec.Contents {
		go GoWalk(e, rootFileRec.Path, fileRecCh, doneCh)
	}

	// While we have outstanding go routines, continue reading from fileRecCh and insert FileRec pointers to the
	// designated slices.
	for i := 0; i < len(rootFileRec.Contents); {
		select {
		case fr := <-fileRecCh:
			if !fr.FileInfo.IsDir() {
				bigFiles = InsertSorted(bigFiles, fr, *resultLimit)
			} else {
				bigDirs = InsertSorted(bigDirs, fr, *resultLimit)
			}
		case _ = <-doneCh:
			i++
		}
	}

	// TODO: nicer output
	fmt.Println()
	fmt.Println("Big Dirs:")
	fmt.Println("---------")
	for _, e := range bigDirs {
		fmt.Println(e)
	}
	fmt.Println("Big Files:")
	fmt.Println("----------")
	for _, e := range bigFiles {
		fmt.Println(e)
	}
}
