package cotfs

import (
	"bazil.org/fuse"
	"bazil.org/fuse/fs"
	"database/sql"
	"errors"
	"fmt"
	"github.com/cfagiani/cotfs/internal/pkg/db"
	"github.com/cfagiani/cotfs/internal/pkg/metadata"
	"github.com/cfagiani/cotfs/internal/pkg/storage"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

var fileName = "myfile"
var fileNameWithExt = "yourfile.exe"
var threeTag = []metadata.TagInfo{
	{Text: "abc"},
	{Text: "def"},
	{Text: "ghi"},
}
var testMount = fmt.Sprintf("%cmymnt%ctmp", os.PathSeparator, os.PathSeparator)
var testContent = "file contents"

// Verifies the Root method returns a valid FS instance.
func TestFS_Root(t *testing.T) {
	metaDb, storageSys := getMockFixtures(t)
	defer metaDb.Close()
	fs := &FS{
		database:      metaDb,
		storageSystem: storageSys,
		mountPoint:    testMount,
	}
	node, err := fs.Root()
	if err != nil || node == nil {
		t.Error("Could not get filesystem root")
	} else {
		//make sure we get a Directory with nil for the path
		dir, ok := node.(*Dir)
		if !ok {
			t.Error("Expected type of root node to be Dir")
		} else {
			if dir.mountPoint != testMount {
				t.Error("Mount point not pushed down into root node")
			}
			if dir.storageSystem == nil {
				t.Error("Storage subystem not pushed down into root node")
			}
			if dir.path != nil {
				t.Errorf("Expected nil for path of root node but got array of length %d", len(dir.path))
			}
		}
	}
}

// Verifies readDirAll returns a list of directory contents.
func TestDir_ReadDirAll(t *testing.T) {
	metaDb, storageSys := getMockFixtures(t)
	defer metaDb.Close()
	tags := createTags(metaDb, 3, 3)
	// tag some files

	oneTagFile, _ := db.CreateFileInPath(metaDb, "one", "path1", []metadata.TagInfo{tags[0][1]})
	twoTagFile, _ := db.CreateFileInPath(metaDb, "one", "path2", []metadata.TagInfo{tags[0][1], tags[1][1]})
	conditions := []struct {
		path          []metadata.TagInfo
		expectedDirs  []metadata.TagInfo
		expectedFiles []metadata.FileInfo
	}{
		{nil, flatten(tags), nil}, // top-level directory
		{[]metadata.TagInfo{tags[0][0]}, []metadata.TagInfo{tags[1][0], tags[2][0]}, nil},
		{[]metadata.TagInfo{tags[0][1]}, []metadata.TagInfo{tags[1][1], tags[2][1]}, []metadata.FileInfo{oneTagFile, twoTagFile}},
		{[]metadata.TagInfo{tags[0][1], tags[1][1]}, []metadata.TagInfo{tags[2][1]}, []metadata.FileInfo{twoTagFile}},
		{[]metadata.TagInfo{tags[0][0], tags[1][0], tags[2][0]}, nil, nil},
	}

	for _, condition := range conditions {
		// create the Directory
		dir := &Dir{
			database:      metaDb,
			mountPoint:    testMount,
			path:          condition.path,
			storageSystem: storageSys,
		}
		entries, err := dir.ReadDirAll(nil)
		if err != nil {
			t.Errorf("Could not read directory: %v", err)
		} else {
			fileCount := 0
			dirCount := 0
			for _, entry := range entries {
				if entry.Type == fuse.DT_Dir {
					dirCount++
					// make sure it's in the expected list
					if !containsDir(entry, condition.expectedDirs) {
						t.Errorf("Found unexpected directory %s", entry.Name)
					}
				} else {
					fileCount++
					if !containsFile(entry, condition.expectedFiles) {
						t.Errorf("Found unexpected file %s", entry.Name)
					}
				}
			}
			if fileCount != len(condition.expectedFiles) {
				t.Errorf("Expected %d files but found %d", len(condition.expectedFiles), fileCount)
			}
			if dirCount != len(condition.expectedDirs) {
				t.Errorf("Expected %d dirs but found %d", len(condition.expectedDirs), dirCount)
			}
		}
	}
}

// Verifies we can lookup directory entries by name.
func TestDir_Lookup(t *testing.T) {
	metaDb, storageSys := getMockFixtures(t)
	defer metaDb.Close()
	tags := createTags(metaDb, 3, 3)
	file1, _ := db.CreateFileInPath(metaDb, "fileInPath", "path1", []metadata.TagInfo{tags[0][1]})
	conditions := []struct {
		name         string
		path         []metadata.TagInfo
		expectedNode fs.Node
	}{
		{file1.Name, []metadata.TagInfo{tags[0][1]}, &File{fileInfo: file1}},
		{file1.Name, nil, nil},
		{file1.Name, []metadata.TagInfo{tags[0][1], tags[1][1], tags[2][1]}, nil},
		{"notThere", []metadata.TagInfo{tags[0][1]}, nil},
		{tags[1][1].Text, []metadata.TagInfo{tags[0][1]}, &Dir{path: []metadata.TagInfo{tags[0][1], tags[1][1]}}},
	}

	for _, condition := range conditions {
		// create the Directory
		dir := &Dir{
			database:      metaDb,
			mountPoint:    testMount,
			path:          condition.path,
			storageSystem: storageSys,
		}
		node, err := dir.Lookup(nil, &fuse.LookupRequest{Name: condition.name}, nil)

		if condition.expectedNode == nil {
			if node != nil || err != fuse.ENOENT {
				t.Errorf("Expected lookup of %s to give NOENT error", condition.name)
			}
		} else {
			file, ok := node.(*File)
			if ok {
				//verify the required fields get populated
				if file.storage == nil {
					t.Error("Storage member of file struct was nil")
				}
				//if it's a file, check the file matches the info
				expectedFile, ok := condition.expectedNode.(*File)
				if !ok {
					t.Error("Got a file node but didn't expect one")
				}
				if expectedFile.fileInfo.Name != file.fileInfo.Name {
					t.Errorf("Expected lookup to find %s but got %s", expectedFile.fileInfo.Name, file.fileInfo.Name)
				}
			} else {
				dir, ok := node.(*Dir)
				if ok {
					if dir.storageSystem == nil || dir.database == nil || dir.path == nil {
						t.Error("Dir structure contained nil fields")
					}
					if dir.mountPoint != testMount {
						t.Errorf("Expected dir to have %s as mountPoint but found %s", testMount, dir.mountPoint)
					}
					expectedDir, ok := condition.expectedNode.(*Dir)
					if !ok {
						t.Error("Got a dir node but didn't expect one")
					}
					if len(dir.path) != len(expectedDir.path) {
						t.Errorf("Expected dir to have path of length %d but found %d", len(expectedDir.path), len(dir.path))
					} else {
						for _, expectedTag := range expectedDir.path {
							found := false
							for _, foundTag := range dir.path {
								if foundTag.Id == expectedTag.Id {
									found = true
									break
								}
							}
							if !found {
								t.Errorf("The tag %s was missing in the dir path", expectedTag.Text)
							}
						}
					}
				}
			}
		}
	}
}

// Verifies mkdir creates tags
func TestDir_Mkdir(t *testing.T) {
	metaDb, storageSys := getMockFixtures(t)
	defer metaDb.Close()
	tags := createTags(metaDb, 1, 1)
	conditions := []struct {
		name string
		path []metadata.TagInfo
	}{
		{"topLevelTag", nil},
		{"nestedTag", tags[0]},
		{tags[0][0].Text, tags[0]},
	}
	for _, condition := range conditions {
		dir := &Dir{
			database:      metaDb,
			mountPoint:    testMount,
			path:          condition.path,
			storageSystem: storageSys,
		}
		node, err := dir.Mkdir(nil, &fuse.MkdirRequest{Name: condition.name})
		if err != nil {
			t.Errorf("Could not mkdir: %v", err)
		} else {
			dirNode, ok := node.(*Dir)
			if !ok {
				t.Error("Could not convert returned node to Dir")
			} else {
				if dirNode.mountPoint != testMount || dirNode.database == nil || dirNode.storageSystem == nil {
					t.Error("Required fields of dir not populated")
				}
				// path should contain the name we created
				found := false
				for _, tag := range dirNode.path {
					if tag.Text == condition.name {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Dir returned from mkdir should include %s", condition.name)
				}
			}
		}
	}
}

// Verifies remove handles tags correctly
func TestDir_RemoveTag(t *testing.T) {
	metaDb, storageSys := getMockFixtures(t)
	defer metaDb.Close()
	tags := createTags(metaDb, 3, 3)
	db.CreateFileInPath(metaDb, "singleTagFile", "path1", []metadata.TagInfo{tags[0][0]})
	db.CreateFileInPath(metaDb, "multiTagFile", "path2", []metadata.TagInfo{tags[0][0], tags[1][1]})
	conditions := []struct {
		path           []metadata.TagInfo
		name           string
		expectedResult error
	}{
		{nil, tags[0][0].Text, fuse.Errno(syscall.ENOTEMPTY)},
		{nil, tags[0][1].Text, nil},
		{[]metadata.TagInfo{tags[0][2]}, tags[1][2].Text, nil},
		{[]metadata.TagInfo{tags[0][2]}, "not there", fuse.ENOENT},
		{nil, "still not there", fuse.ENOENT},
		{nil, tags[1][1].Text, nil},
	}
	var deletedTags []string
	for _, condition := range conditions {
		dir := &Dir{
			database:      metaDb,
			mountPoint:    testMount,
			path:          condition.path,
			storageSystem: storageSys,
		}
		result := dir.Remove(nil, &fuse.RemoveRequest{Name: condition.name, Dir: true})
		if result == nil {
			deletedTags = append(deletedTags, condition.name)
		}
		if result != condition.expectedResult {
			t.Errorf("Unexpected result when attempting to remove %s", condition.name)
		}
	}
	remainingTags, _ := db.GetAllTags(metaDb)
	for _, tag := range remainingTags {
		for _, name := range deletedTags {
			if tag.Text == name {
				t.Errorf("Expected tag %s to have been deleted, but it abides.", name)
			}
		}
	}
}

func TestDir_RemoveFile(t *testing.T) {
	metaDb, storageSys := getMockFixtures(t)
	defer metaDb.Close()
	tags := createTags(metaDb, 3, 3)
	file1, _ := db.CreateFileInPath(metaDb, "singleTagFile", "path1", []metadata.TagInfo{tags[0][0]})
	file2, _ := db.CreateFileInPath(metaDb, "multiTagFile", "path2", []metadata.TagInfo{tags[0][0], tags[1][1]})
	fileCount := 3
	nameBase := "baseFile"
	for i := 0; i < fileCount; i++ {
		db.CreateFileInPath(metaDb, fmt.Sprintf("%s%d", nameBase, i), fmt.Sprintf("pathx%d", i), []metadata.TagInfo{tags[0][0]})
	}
	conditions := []struct {
		path           []metadata.TagInfo
		name           string
		expectedResult error
	}{
		{nil, file1.Name, fuse.ENOENT},
		{[]metadata.TagInfo{tags[0][0]}, file1.Name, nil},
		{[]metadata.TagInfo{tags[1][1]}, "notThere", fuse.ENOENT},
		{[]metadata.TagInfo{tags[0][0]}, file2.Name, nil},
		{[]metadata.TagInfo{tags[1][1]}, file2.Name, nil},
		{[]metadata.TagInfo{tags[0][0]}, fmt.Sprintf("%s*", nameBase), nil},
	}
	for _, condition := range conditions {
		dir := &Dir{
			database:      metaDb,
			mountPoint:    testMount,
			path:          condition.path,
			storageSystem: storageSys,
		}
		result := dir.Remove(nil, &fuse.RemoveRequest{Name: condition.name, Dir: false})
		if result != condition.expectedResult {
			t.Errorf("Unexpected result when attempting to remove %s", condition.name)
		}
	}
	// we should have removed everything; verify that we did
	for i := 0; i < len(tags); i++ {
		for j := 0; j < len(tags[i]); j++ {
			files, err := db.GetFilesWithTags(metaDb, []metadata.TagInfo{tags[i][j]}, "")
			if err != nil {
				t.Errorf("Error while looking for files with tag %s: %v", tags[i][j].Text, err)
			} else {
				if files != nil && len(files) > 0 {
					t.Errorf("Expected tag %s to have 0 files. Found %d", tags[i][j].Text, len(files))
				}
			}

		}
	}
}

// Verifies we can symlink within the filesystem
func TestDir_Symlink(t *testing.T) {
	metaDb, storageSys := getMockFixtures(t)
	defer metaDb.Close()
	tags := createTags(metaDb, 3, 3)
	file1, _ := db.CreateFileInPath(metaDb, "singleTagFile", fmt.Sprintf("%cblah", os.PathSeparator), []metadata.TagInfo{tags[0][0]})
	db.CreateFileInPath(metaDb, "singleTagFile2", "path2", []metadata.TagInfo{tags[0][0]})
	conditions := []struct {
		path          []metadata.TagInfo
		target        string
		expectedName  string
		expectedError error
	}{
		{nil, fmt.Sprintf("%s%c%s%c%s", testMount, os.PathSeparator, tags[0][0].Text, os.PathSeparator, file1.Name), "", fuse.EPERM},
		{[]metadata.TagInfo{tags[0][1]}, fmt.Sprintf("%s%c%s%c%s*", testMount, os.PathSeparator, tags[0][0].Text, os.PathSeparator, file1.Name), "", fuse.EPERM},
		{[]metadata.TagInfo{tags[0][1]}, fmt.Sprintf("%s%c%s%c%s", testMount, os.PathSeparator, tags[0][0].Text, os.PathSeparator, file1.Name), file1.Name, nil},
		{[]metadata.TagInfo{tags[0][1]}, fmt.Sprintf("%s%c%s%cnotThere", testMount, os.PathSeparator, tags[0][0].Text, os.PathSeparator), "", fuse.ENOENT},
		{[]metadata.TagInfo{tags[0][1]}, fmt.Sprintf("%croot%csomeDIR", os.PathSeparator, os.PathSeparator), "", fuse.EPERM},
		{[]metadata.TagInfo{tags[0][2]}, fmt.Sprintf("%s%c%s", file1.Path, os.PathSeparator, file1.Name), file1.Name, nil},
		{[]metadata.TagInfo{tags[0][2]}, fmt.Sprintf("%croot%cSomeFile", os.PathSeparator, os.PathSeparator), "SomeFile", nil},
	}
	for _, condition := range conditions {
		dir := &Dir{
			database:      metaDb,
			mountPoint:    testMount,
			path:          condition.path,
			storageSystem: storageSys,
		}

		node, err := dir.Symlink(nil, &fuse.SymlinkRequest{Target: condition.target})
		if condition.expectedError != nil && condition.expectedError != err {
			t.Errorf("Unexpected error during link %v", err)
		} else if condition.expectedError == nil {
			fileNode, ok := node.(*File)
			if !ok {
				t.Error("Symlink should return a file")
			}
			if fileNode.fileInfo.Name != condition.expectedName {
				t.Errorf("Expceted file to be named %s but found %s", condition.expectedName, fileNode.fileInfo.Name)
			}
		}
	}
}

// Verifies we can read a file
func TestFile_Open(t *testing.T) {
	metaDb, storageSys := getMockFixtures(t)
	defer metaDb.Close()
	fileInfo := &File{
		fileInfo: metadata.FileInfo{Name: "someName", Path: "somePath"},
		storage:  storageSys,
	}
	fileHandle, err := fileInfo.Open(nil, nil, nil)
	if err != nil {
		t.Errorf("Could not open file: %v", err)
	}
	if fileHandle == nil {
		t.Error("Got a nil file handle from open")
	}

	// ensure that file errors are propagated
	fileInfo = &File{
		fileInfo: metadata.FileInfo{Name: "thisWillERROR"},
		storage:  storageSys,
	}
	_, err = fileInfo.Open(nil, nil, nil)
	if err == nil {
		t.Error("Expected and error from Open bug did not get one")
	}
}

func TestFileHandle_Read(t *testing.T) {
	metaDb, storageSys := getMockFixtures(t)
	defer metaDb.Close()
	fileInfo := &File{
		fileInfo: metadata.FileInfo{Name: "someName", Path: "somePath"},
		storage:  storageSys,
	}
	sizesToRead := []int{1, 5, 10, len(testContent), len(testContent) + 10}

	for _, size := range sizesToRead {
		fh, _ := fileInfo.Open(nil, nil, nil)
		fileHandle := fh.(*FileHandle)
		response := &fuse.ReadResponse{}
		err := fileHandle.Read(nil, &fuse.ReadRequest{Size: size}, response)
		if err != nil {
			t.Errorf("Unexpected error reading file: %v", err)
		}

		if len(response.Data) != size {
			t.Errorf("Expected data read to be %d bytes but was %d", size, len(response.Data))
		}

	}

}

// Verifies hard-linking works within the filesystem
func TestDir_Link(t *testing.T) {
	metaDb, storageSys := getMockFixtures(t)
	defer metaDb.Close()
	tags := createTags(metaDb, 3, 3)
	file1, _ := db.CreateFileInPath(metaDb, "singleTagFile", "path1", []metadata.TagInfo{tags[0][0]})
	conditions := []struct {
		path          []metadata.TagInfo
		source        fs.Node
		expectedError error
	}{
		{nil, &File{fileInfo: file1}, fuse.EPERM},
		{nil, &Dir{path: tags[0]}, fuse.EPERM},
		{[]metadata.TagInfo{tags[0][1]}, &Dir{path: tags[0]}, fuse.EPERM},
		{[]metadata.TagInfo{tags[0][1]}, &File{fileInfo: file1}, nil},
	}
	for _, condition := range conditions {
		dir := &Dir{
			database:      metaDb,
			mountPoint:    testMount,
			path:          condition.path,
			storageSystem: storageSys,
		}
		node, err := dir.Link(nil, &fuse.LinkRequest{}, condition.source)
		if condition.expectedError != nil && condition.expectedError != err {
			t.Errorf("Unexpected error during link %v", err)
		} else if condition.expectedError == nil {
			if condition.source != node {
				t.Error("Expected link to return the same node as source on success")
			}
		}
	}
}

// Tests conversion of path strings that may or may be relative to absolute paths, including those that use relative
// "parent dir" (..) to traverse outside of the mount point.
func TestConvertToAbsolutePath(t *testing.T) {
	conditions := []struct {
		tags     []metadata.TagInfo
		newPath  string
		dirPath  string
		fileName string
	}{
		{ // filename with no extension
			nil,
			fmt.Sprintf("%cabc%cdef%c%s", os.PathSeparator, os.PathSeparator, os.PathSeparator, fileName),
			fmt.Sprintf("%cabc%cdef", os.PathSeparator, os.PathSeparator),
			fileName,
		},
		{ // filename with extension
			nil,
			fmt.Sprintf("%cabc%cdef%c%s", os.PathSeparator, os.PathSeparator,
				os.PathSeparator, fileNameWithExt),
			fmt.Sprintf("%cabc%cdef", os.PathSeparator, os.PathSeparator),
			fileNameWithExt,
		},
		{ // Absolute paths with embedded .. don't get translated since it's already absolute
			nil,
			fmt.Sprintf("%cabc%c..%cdef%c%s", os.PathSeparator, os.PathSeparator,
				os.PathSeparator, os.PathSeparator, fileName),
			fmt.Sprintf("%cdef", os.PathSeparator),
			fileName,
		},
		{ // tags get converted to path rooted at mount point and pre-pended to newPath
			threeTag,
			fmt.Sprintf("xyz%c%s", os.PathSeparator, fileName),
			fmt.Sprintf("%s%cabc%cdef%cghi%cxyz", testMount, os.PathSeparator,
				os.PathSeparator, os.PathSeparator, os.PathSeparator),
			fileName,
		},
		{ // relative paths are evaluated against the tags
			threeTag,
			fmt.Sprintf("..%c%s", os.PathSeparator, fileName),
			fmt.Sprintf("%s%cabc%cdef", testMount, os.PathSeparator, os.PathSeparator),
			fileName,
		},
		{ // relative paths can end up in another filesystem
			threeTag,
			fmt.Sprintf("..%c..%c..%c..%c..%cblerg%c%s", os.PathSeparator, os.PathSeparator,
				os.PathSeparator, os.PathSeparator, os.PathSeparator, os.PathSeparator, fileName),
			fmt.Sprintf("%cblerg", os.PathSeparator),
			fileName,
		},
	}
	for _, condition := range conditions {
		resultDir, resultFile := convertToAbsolutePath(condition.tags, condition.newPath, testMount)
		if resultDir != condition.dirPath {
			t.Errorf("Expected to convert %s to %s but got %s", condition.newPath, condition.dirPath, resultDir)
		}
		if resultFile != condition.fileName {
			t.Errorf("Expected to extract filename %s from %s but got %s", condition.fileName, condition.newPath, resultFile)
		}
	}
}

// Validates that the appendIfNotFound method does not create duplicates in the array.
func TestAppendIfNotFound(t *testing.T) {
	conditions := []struct {
		tags   []metadata.TagInfo
		newTag metadata.TagInfo
		len    int
	}{
		{
			[]metadata.TagInfo{{Text: "a"}, {Text: "b"}, {Text: "c"}},
			metadata.TagInfo{Text: "d"},
			4,
		},
		{
			[]metadata.TagInfo{{Text: "a"}, {Text: "b"}, {Text: "c"}},
			metadata.TagInfo{Text: "a"},
			3,
		},
		{
			nil,
			metadata.TagInfo{Text: "d"},
			1,
		},
	}
	for _, condition := range conditions {
		result := appendIfNotFound(condition.tags, condition.newTag)
		if len(result) != condition.len {
			t.Errorf("Expected to get %d elements but got %d", condition.len, len(result))
		}
		// if we did append, make sure last element is the one we appended
		if len(result) > len(condition.tags) && result[len(result)-1].Text != condition.newTag.Text {
			t.Errorf("Expected %s to be appended to end but it was not", condition.newTag.Text)
		}

	}
}

func containsDir(entry fuse.Dirent, dirs []metadata.TagInfo) bool {
	if dirs == nil {
		return false
	}
	for _, node := range dirs {
		if node.Text == entry.Name {
			return true
		}
	}
	return false
}

func containsFile(entry fuse.Dirent, files []metadata.FileInfo) bool {
	if files == nil {
		return false
	}
	for _, node := range files {
		if node.Name == entry.Name {
			return true
		}
	}
	return false
}

// creates tags tags and their associations
func createTags(database *sql.DB, levels int, tagsPerLevel int) [][]metadata.TagInfo {
	tags := make([][]metadata.TagInfo, levels)
	for i := 0; i < levels; i++ {
		tags[i] = make([]metadata.TagInfo, tagsPerLevel)
		for j := 0; j < tagsPerLevel; j++ {
			var context []metadata.TagInfo
			if i > 0 {
				for k := i - 1; k >= 0; k-- {
					context = append(context, tags[k][j])
				}
			}
			tags[i][j], _ = db.AddTag(database, fmt.Sprintf("tag%d-%d", i, j), context)
		}

	}
	return tags
}

func flatten(tags [][]metadata.TagInfo) []metadata.TagInfo {
	var flattened []metadata.TagInfo
	for i := 0; i < len(tags); i++ {
		for j := 0; j < len(tags[i]); j++ {
			flattened = append(flattened, tags[i][j])
		}
	}
	return flattened
}

// Returns an open in-memory database (callers should close when done) and a mocked FileStorage implementation.
func getMockFixtures(t *testing.T) (*sql.DB, storage.FileStorage) {
	database, err := db.Open("file::memory:?cache=shared")
	if err != nil {
		t.Errorf("Could not open database")
	}
	return database, MockFileStorage{}
}

// Mock file storage subsystem. Used only for testing.
type MockFileStorage struct{}

type MockFile struct {
	name string
}

func (MockFileStorage) Open(name string) (storage.File, error) {
	if strings.Index(name, "ERROR") >= 0 {
		return nil, errors.New("Generated error")
	} else {
		return MockFile{}, nil
	}
}

func (MockFileStorage) Stat(name string) (os.FileInfo, error) {
	if strings.Index(name, "ERROR") >= 0 {
		return nil, errors.New("Generated error")
	} else {
		return MockFile{
			name: name,
		}, nil
	}
}

func (f MockFile) Stat() (os.FileInfo, error) {
	return f, nil
}

func (MockFile) Close() error { return nil }
func (MockFile) Read(p []byte) (n int, err error) {
	for idx, _ := range p {
		if idx >= len(testContent) {
			return len(testContent), nil
		}
		p[idx] = testContent[idx]

	}
	return len(p), nil
}

// FileInfo methods
func (MockFile) Size() int64 {
	return int64(len(testContent))
}

func (f MockFile) Name() string {
	return f.name
}

func (f MockFile) Mode() os.FileMode {
	if strings.Index(f.name, "DIR") >= 0 {
		return os.ModeDir | 0755
	} else {
		return 0755
	}
}

func (MockFile) ModTime() time.Time {
	return time.Time{}
}

func (f MockFile) IsDir() bool {
	return strings.Index(f.name, "DIR") >= 0
}

func (f MockFile) Sys() interface{} {
	return syscall.Stat_t{
		Ctimespec: syscall.Timespec{0, 0},
	}
}
