package cotfs

import (
	"fmt"
	"github.com/cfagiani/cotfs/internal/pkg/metadata"
	"os"
	"testing"
)

var fileName = "myfile"
var fileNameWithExt = "yourfile.exe"
var threeTag = []metadata.TagInfo{
	{Text: "abc"},
	{Text: "def"},
	{Text: "ghi"},
}
var testMount = fmt.Sprintf("%cmymnt/tmp", os.PathSeparator)

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
			fmt.Sprintf("%cabc%c..%cdef", os.PathSeparator, os.PathSeparator, os.PathSeparator),
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
	mountPoint = testMount
	for _, condition := range conditions {
		resultDir, resultFile := convertToAbsolutePath(condition.tags, condition.newPath)
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
