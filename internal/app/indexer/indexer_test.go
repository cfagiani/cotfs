package indexer

import (
	"database/sql"
	"fmt"
	"github.com/cfagiani/cotfs/internal/pkg/db"
	"github.com/cfagiani/cotfs/internal/pkg/metadata"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Verifies we can index a local directory correctly.
func TestIndexLocalDirectory(t *testing.T) {
	database := getDb(t)
	defer database.Close()

	// load the tags we'll use
	tagCache := initTagCache(database, map[string][]string{
		".txt": {"text"},
	})
	err := indexLocalDirectory(database, getTestDataDirectory(), tagCache)
	if err != nil {
		t.Errorf("Could not index %s is that the right directory? %v", getTestDataDirectory(), err)
	}
	// now make sure we get the files we expect
	conditions := []struct {
		tag           metadata.TagInfo
		expectedFiles []string
	}{
		{tagCache[".txt"][0], []string{"one.txt", "two.txt", "three.txt"}},
		{tagCache[defaultTag][0], []string{"four.md"}},
	}
	for _, condition := range conditions {
		files, _ := db.GetFilesWithTags(database, []metadata.TagInfo{condition.tag}, "")
		if len(files) != len(condition.expectedFiles) {
			t.Errorf("Expected %d files to be tagged with %s but found %d",
				len(condition.expectedFiles), condition.tag.Text, len(files))
		} else {
			for _, file := range files {
				found := false
				for _, expectedFile := range condition.expectedFiles {
					if expectedFile == file.Name {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected to find %s tagged with %s but it was not", file.Name, condition.tag.Text)
				}
			}
		}
	}
}

// Verifies we get the right tags based on file extension
func TestInferTagsFromFile(t *testing.T) {
	// first set up the tag cache
	tagCache := map[string][]metadata.TagInfo{
		".jpg":     {{Text: "image"}},
		".xlsx":    {{Text: "document"}, {Text: "spreadsheet"}},
		defaultTag: {{Text: "defaultTag"}},
	}
	conditions := []struct {
		path string
		tags []metadata.TagInfo
	}{
		{"/test/blah/nothing", tagCache[defaultTag]},
		{"test.jpg", tagCache[".jpg"]},
		{"test.xls", tagCache[defaultTag]},
		{"test.xlsx", tagCache[".xlsx"]},
		{"/test.jpg/test.xlsx", tagCache[".xlsx"]},
	}
	for _, condition := range conditions {
		tags := inferTagsFromFile(condition.path, tagCache)
		if len(tags) != len(condition.tags) {
			t.Errorf("Expected to find %d tags but foudn %d for %s", len(condition.tags), len(tags), condition.path)
		}
		for i, v := range tags {
			if v.Text != condition.tags[i].Text {
				t.Errorf("Expected tag %d to be %s for %s but got %s", i, condition.tags[i].Text, condition.path, v.Text)
			}
		}
	}
}

// Tests that the tag cache creates tags in the metadata db and stores them in a map.
func TestInitTagCache(t *testing.T) {
	database := getDb(t)
	defer database.Close()
	tagsToMap := map[string][]string{
		"one":   {"a"},
		"two":   {"a", "b"},
		"three": {"d", "e", "f"},
	}
	cachedTags := initTagCache(database, tagsToMap)
	// now ensure we got what we expected
	for key, val := range tagsToMap {
		if len(val) != len(cachedTags[key]) {
			t.Errorf("Expected key %s to have %d tags but found %d", key, len(val), len(cachedTags[key]))
		}
		for idx, tag := range val {
			if tag != cachedTags[key][idx].Text {
				t.Errorf("Expected key %s to have tag %s at position %d but foudn %s",
					key, tag, idx, cachedTags[key][idx].Text)
			}
		}
	}
	// also make sure we have a default tag
	_, ok := cachedTags[defaultTag]
	if !ok {
		t.Error("Default tag not found in cache")
	}
	// check that we don't have a duplicate
	if cachedTags["one"][0].Id != cachedTags["two"][0].Id {
		t.Errorf("Expected tag %s to have same id but they were different", cachedTags["one"][0].Text)
	}
}

// Helper to get a reference to an in-memory database. Callers should close the db when done.
func getDb(t *testing.T) *sql.DB {
	// need shared cache to allow different connections to use same in-memory db
	database, err := db.Open("file::memory:?cache=shared")
	if err != nil {
		t.Errorf("Could not open database")
	}
	return database
}

// Helper to build the path to the test directory. Note this may have to change if this test file is relocated since
// we use relative .. paths to traverse to the data/indexer directory
func getTestDataDirectory() string {
	_, thisFilename, _, _ := runtime.Caller(0)
	testDir := filepath.Dir(thisFilename)
	return filepath.Clean(fmt.Sprintf("%s%c..%c..%c..%ctest%cdata%cindexer", testDir, os.PathSeparator,
		os.PathSeparator, os.PathSeparator, os.PathSeparator, os.PathSeparator, os.PathSeparator))
}
