package db

import (
	"database/sql"
	"errors"
	"fmt"
	"github.com/cfagiani/cotfs/internal/pkg/metadata"
	"strings"
	"testing"
)

// Validates adding top-level tags work and do not create duplicates
func TestAddTag(t *testing.T) {
	db := getDb(t)
	defer db.Close()
	tags, err := GetAllTags(db)
	if err != nil || (tags != nil && len(tags) > 0) {
		t.Errorf("Tag database should start off empty")
	}

	tagText := "toptag"

	// add a top-level tag and ensure it's inserted
	tagInfo, err := AddTag(db, tagText, nil)
	if err != nil || tagInfo.Id == metadata.UnknownTag.Id {
		t.Errorf("could not insert tag")
	}
	// ensure it's there
	tags, _ = GetAllTags(db)
	if len(tags) != 1 {
		t.Errorf("Expected 1 tag but found %d", len(tags))
	}
	if tags[0].Id != tagInfo.Id || tags[0].Text != tagInfo.Text || tagInfo.Text != tagText {
		t.Errorf("Tag found did not match values from insert")
	}

	//ensure we don't get a duplicate if we try to insert again
	otherTagInfo, err := AddTag(db, tagText, nil)
	if otherTagInfo.Id != tagInfo.Id {
		t.Errorf("Expected to get id %d back from duplicate insert but found %d", tagInfo.Id, otherTagInfo.Id)
	}

	// now insert a child tag and ensure it is associated
	childTag, err := AddTag(db, "child", []metadata.TagInfo{tagInfo})
	if childTag.Id == metadata.UnknownTag.Id {
		t.Errorf("Could not insert child tag")
	}

}

// Tests that we can remove a tag association but keep the tag records.
func TestUnassociateTag(t *testing.T) {
	db := getDb(t)
	defer db.Close()

	tags, err := createTags(db, "a", 2)
	if err != nil {
		t.Errorf("Could not create tags %s", err)
	}

	// first verify the two tags are associated, regardless of which way we look them up
	foundTag, _ := GetCoincidentTag(db, tags[0].Text, tags[1].Text)
	if foundTag.Id == metadata.UnknownTag.Id {
		t.Errorf("Tags not associated when looked up from %s to %s", tags[0].Text, tags[1].Text)
	}
	foundTag, _ = GetCoincidentTag(db, tags[1].Text, tags[0].Text)
	if foundTag.Id == metadata.UnknownTag.Id {
		t.Errorf("Tags not associated when looked up from %s to %s", tags[1].Text, tags[0].Text)
	}

	// now unassociate
	err = UnassociateTag(db, tags[0], tags[1])
	if err != nil {
		t.Error(err)
	}
	foundTag, _ = GetCoincidentTag(db, tags[0].Text, tags[1].Text)
	if foundTag.Id != metadata.UnknownTag.Id {
		t.Error("Expected not to find coincident tag")
	}
	foundTag, _ = GetCoincidentTag(db, tags[1].Text, tags[0].Text)
	if foundTag.Id != metadata.UnknownTag.Id {
		t.Error("Expected not to find coincident tag")
	}

	// make sure the tags are still there
	for _, tag := range tags {
		a, _ := GetTag(db, tag.Text)
		if a.Id != tag.Id {
			t.Errorf("Could not find tag %s", tag.Text)
		}
	}
}

// Verifies we can get a single co-incident tag regardless of the order of the lookup.
func TestGetCoincidentTag(t *testing.T) {
	db := getDb(t)
	defer db.Close()

	tags, err := createTags(db, "a", 2)
	if err != nil {
		t.Errorf("Could not create tags %s", err)
	}

	// verify the two tags are associated, regardless of which way we look them up
	foundTag, _ := GetCoincidentTag(db, tags[0].Text, tags[1].Text)
	if foundTag.Id == metadata.UnknownTag.Id {
		t.Errorf("Tags not associated when looked up from %s to %s", tags[0].Text, tags[1].Text)
	}
	foundTag, _ = GetCoincidentTag(db, tags[1].Text, tags[0].Text)
	if foundTag.Id == metadata.UnknownTag.Id {
		t.Errorf("Tags not associated when looked up from %s to %s", tags[1].Text, tags[0].Text)
	}
	// verify that we don't get any results when we pass in a non-associated tag
	for _, tag := range tags {
		foundTagA, _ := GetCoincidentTag(db, tag.Text, "junk")
		foundTagB, _ := GetCoincidentTag(db, "junk", tag.Text)
		if foundTagA.Id != metadata.UnknownTag.Id || foundTagB.Id != metadata.UnknownTag.Id {
			t.Errorf("Expected not to find a tag associated with 'junk' but we did")
		}
	}
}

// Verifies we can delete tags and their associations
func TestDeleteTag(t *testing.T) {
	db := getDb(t)
	defer db.Close()
	tags, err := createTags(db, "a", 2)
	if err != nil {
		t.Errorf("Could not create tags %s", err)
	}
	// delete a tag
	err = DeleteTag(db, tags[1])
	if err != nil {
		t.Errorf("could not delete tag %s", err)
	}
	// make sure we can't get the tag anymore
	foundTag, err := GetTag(db, tags[1].Text)
	if err != nil {
		t.Errorf("Lookup of delete tag should not cause error")
	}
	if foundTag.Id != metadata.UnknownTag.Id {
		t.Errorf("Get tag should have returned unknownTag but did not")
	}
}

// Verifies we can list co-incident tags with multiple levels
func TestGetCoincidentTags(t *testing.T) {
	db := getDb(t)
	defer db.Close()
	levels := 100
	tags, err := createTags(db, "a", levels)
	if err != nil {
		t.Errorf("Could not create tags %s", err)
	}
	// get all co-incident tags
	for i := levels - 1; i > 0; i-- {
		coincident, _ := GetCoincidentTags(db, tags[:i], "")
		if len(coincident) != levels-i {
			t.Errorf("Expected %d co-incident tags but found %d", levels-i, len(coincident))
		}
	}

	// filter co-incident by name exact match
	coincident, _ := GetCoincidentTags(db, tags[:1], "a2")
	if len(coincident) != 1 {
		t.Errorf("Expected 1 tag to match but got %d", len(coincident))
	}
	if coincident[0].Text != "a2" {
		t.Errorf("Expected tag to have text a2 but got %s", coincident[0].Text)
	}

	// filter with wildcard
	coincident, _ = GetCoincidentTags(db, tags[:1], "a2*")
	if len(coincident) != 11 {
		t.Errorf("Expected 11 tags to match but got %d", len(coincident))
	}
	for _, tag := range coincident {
		if strings.Index(tag.Text, "a2") != 0 {
			t.Errorf("Expected tag text to start with a2 but found %s", tag.Text)
		}
	}
}

// Verifies we can find tag by name
func TestFindTag(t *testing.T) {
	db := getDb(t)
	defer db.Close()
	tags, err := createTags(db, "a", 3)
	if err != nil {
		t.Errorf("Could not create tags %s", err)
	}

	for _, tag := range tags {
		foundTag, err := FindTag(db, tag.Text)
		if err != nil {
			t.Errorf("Could not lookup tag with name %s: %s", tag.Text, err)
		}
		if foundTag.Id != tag.Id {
			t.Errorf("Lookup of tag %s found id %d but expected %d", tag.Text, foundTag.Id, tag.Id)
		}
	}

	// make sure a lookup doesn't return error for not found
	fakeTag, err := FindTag(db, "junk")
	if err != nil {
		t.Errorf("Find should not return error for not found, but got %s", err)
	}
	if fakeTag.Id != metadata.UnknownTag.Id {
		t.Errorf("Find on non-existant tag should return unknown id but got %d", fakeTag.Id)
	}
}

// Validates we can save file metadata and associate it with tags on save.
func TestCreateFileInPath(t *testing.T) {
	// first create a path
	db := getDb(t)
	defer db.Close()
	tags, err := createTags(db, "a", 3)
	if err != nil {
		t.Errorf("Could not create tags %s", err)
	}
	name := "myname"
	path := "mypath"
	createdFile, err := CreateFileInPath(db, name, path, tags[:2])
	if err != nil {
		t.Errorf("Could not create file %s", err)
	}
	if createdFile.Id == metadata.UnknownFile.Id {
		t.Errorf("Could not create file %s", name)
	}

	foundFiles, err := GetFilesWithTags(db, tags[:2], "")
	if err != nil {
		t.Errorf("Could not find file after save: %s", err)
	} else if foundFiles == nil || len(foundFiles) != 1 {
		t.Errorf("Found %d files when 1 was expected", len(foundFiles))
	} else if foundFiles[0].Id != createdFile.Id {
		t.Errorf("Id of found file does not match id from save. Expected %d but got %d",
			createdFile.Id, foundFiles[0].Id)
	} else if foundFiles[0].Name != name || foundFiles[0].Path != path {
		t.Errorf("Metadata on found file does not match what was saved. Expected %s, %s but got %s, %s",
			name, path, foundFiles[0].Name, foundFiles[0].Path)
	}
}

// Validates we can look up files by tags
func TestGetFilesWithTags(t *testing.T) {
	db := getDb(t)
	defer db.Close()
	baseName := "nameBase"
	path := "tmp"
	fileCount := 100
	tags, _, err := createFilesAndTags(db, baseName, path, fileCount, 2)
	if err != nil {
		t.Errorf("Could not create files for test %s", err)
	}

	conditions := []struct {
		tags          []metadata.TagInfo
		name          string
		expectedCount int
	}{
		// get all the files we creates
		{tags[:2],
			"",
			fileCount,
		},
		// try checking a path that should have no files
		{
			tags,
			"",
			0,
		},
		// filter on exact name within the path
		{
			tags[:2],
			fmt.Sprintf("%s%d", baseName, 1),
			1,
		},
		// filter on wildcard name within the path
		{
			tags[:2],
			fmt.Sprintf("%s%d*", baseName, 1),
			11,
		},
		// filter that doesn't match should yield nothing
		{
			tags[:2],
			"junk",
			0,
		},
		// Parent path should still list files
		{
			tags[:1],
			"",
			fileCount,
		},
	}

	// test lookup conditions
	for _, condition := range conditions {
		foundFiles, err := GetFilesWithTags(db, condition.tags, condition.name)
		if err != nil {
			t.Errorf("Could not list tags in path: %s", err)
		} else if len(foundFiles) != condition.expectedCount {
			t.Errorf("Expected to find %d files but got %d", condition.expectedCount, len(foundFiles))
		}
	}
}

// Validates that tagging a file allows it to be found when listing by tags
func TestTagFile(t *testing.T) {
	db := getDb(t)
	defer db.Close()

	tags, files, err := createFilesAndTags(db, "myfile", "mypath", 1, 2)
	if err != nil {
		t.Errorf("Could not create files for test %s", err)
	}
	// ensure we can't find the file when looking with the 3rd tag
	foundFiles, err := GetFilesWithTags(db, tags, "")
	if isFileFound(foundFiles, files[0]) {
		t.Errorf("File %d found when it should no have been", files[0].Id)
	}

	// now tag it and ensure we can find the file
	err = TagFile(db, files[0].Id, tags[2:])
	if err != nil {
		t.Errorf("Could not tag file: %s", err)
	} else {
		foundFiles, err = GetFilesWithTags(db, tags, "")
		if !isFileFound(foundFiles, files[0]) {
			t.Errorf("Expected to find tag id %d but it was not there", files[0].Id)
		}
	}

	// Check error conditions
	conditions := []struct {
		tags []metadata.TagInfo
	}{
		{tags},
		{nil},
		{make([]metadata.TagInfo, 0)},
	}
	for _, condition := range conditions {
		err = TagFile(db, files[0].Id, condition.tags)
		if err != nil {
			t.Errorf("Should not get error, but got %s", err)
		}
	}
}

// Verifies un-tagging a file removes it from a path.
func TestUntagFile(t *testing.T) {
	db := getDb(t)
	defer db.Close()
	tags, files, err := createFilesAndTags(db, "myfile", "mypath", 1, 3)
	if err != nil {
		t.Errorf("Could not create files for test %s", err)
	}
	// untag file
	err = UntagFile(db, files[0].Id, tags[2].Id)
	if err != nil {
		t.Errorf("Could not untag file: %s", err)
	}
	// now make sure we can't find it anymore
	foundFiles, _ := GetFilesWithTags(db, tags, "")
	if isFileFound(foundFiles, files[0]) {
		t.Errorf("File still found after untagging")
	}
	// ensure file is still there, though
	foundFiles, _ = GetFilesWithTags(db, tags[:2], "")
	if !isFileFound(foundFiles, files[0]) {
		t.Errorf("File not found in path")
	}
}

// Verifies we can untag multiple files.
func TestUntagFiles(t *testing.T) {
	db := getDb(t)
	defer db.Close()
	fileCount := 20
	tags, _, err := createFilesAndTags(db, "baseName", "xxx", fileCount, 3)
	if err != nil {
		t.Errorf("Could not create files for test %s", err)
	}
	// remove tags
	err = UntagFiles(db, tags)
	if err != nil {
		t.Errorf("Could not untag file: %s", err)
	}
	// now make sure we can't find any files with all 3 tags
	foundFiles, err := GetFilesWithTags(db, tags, "")
	if len(foundFiles) != 0 {
		t.Errorf("Should have found 0 files but found %d", len(foundFiles))
	}
	// make sure they're still searchable, though
	foundFiles, err = GetFilesWithTags(db, tags[:2], "")
	if len(foundFiles) != fileCount {
		t.Errorf("Expected to find %d files but found %d", fileCount, len(foundFiles))
	}
}

// Verifies find by path/name.
func TestFindFileByAbsPath(t *testing.T) {
	db := getDb(t)
	defer db.Close()
	fileCount := 10
	pathCount := 5
	pathBase := "xxx"
	baseName := "baseName"
	for i := 0; i < pathCount; i++ {
		_, _, err := createFilesAndTags(db, baseName, fmt.Sprintf("%s%d", pathBase, i), fileCount, 3)
		if err != nil {
			t.Errorf("Could not create files for test %s", err)
		}
	}
	// make sure we can find all files
	for i := 0; i < pathCount; i++ {
		pathName := fmt.Sprintf("%s%d", pathBase, i)
		for j := 0; j < fileCount; j++ {
			fileName := fmt.Sprintf("%s%d", baseName, j)
			file, err := FindFileByAbsPath(db, fileName, pathName)
			if err != nil {
				t.Errorf("Could not find file by path: %s", err)
			}
			if file.Name != fileName || file.Path != pathName {
				t.Errorf("Found file does not match. Expected %s,%s but got %s,%s",
					fileName, pathName, file.Name, file.Path)
			}
		}
		// ensure we don't get false matches
		file, err := FindFileByAbsPath(db, "junk", pathName)
		if err != nil {
			t.Errorf("Find should not return an error. Got: %s", err)
		}
		if file.Id != metadata.UnknownFile.Id {
			t.Errorf("Find should have returned unknown, but got %d", file.Id)
		}
	}
}

// Validates that we get the right number of files that only have a single tag.
func TestGetFileCountWithSingleTag(t *testing.T) {
	db := getDb(t)
	defer db.Close()
	fileCount := 10
	tags, _, err := createFilesAndTags(db, "baseName", "xxx", fileCount, 3)
	if err != nil {
		t.Errorf("Could not create files for test %s", err)
	}
	for _, tag := range tags {
		count, err := GetFileCountWithSingleTag(db, tag)
		if err != nil {
			t.Errorf("Could not count files: %s", err)
		}
		if count != 0 {
			t.Errorf("Could should have been 0 but was %d", count)
		}
	}
	_, _, err = createFilesAndTags(db, "baseName2", "xxx", fileCount, 1)
	if err != nil {
		t.Errorf("Could not create files for test %s", err)
	}
	count, err := GetFileCountWithSingleTag(db, tags[0])
	if err != nil {
		t.Errorf("Could not count files: %s", err)
	}
	if count != fileCount {
		t.Errorf("Could should have been %d but was %d", fileCount, count)
	}
}

// Validates we can get the right number of files that have a specific tag.
func TestCountFilesWithTag(t *testing.T) {
	db := getDb(t)
	defer db.Close()
	fileCount := 10
	tags, _, err := createFilesAndTags(db, "baseName", "xxx", fileCount, 3)
	if err != nil {
		t.Errorf("Could not create files for test %s", err)
	}
	_, _, err = createFilesAndTags(db, "baseName", "yyy", fileCount, 2)
	if err != nil {
		t.Errorf("Could not create files for test %s", err)
	}
	extraTag, err := AddTag(db, "zzzz", nil)
	conditions := []struct {
		tag           metadata.TagInfo
		expectedCount int
	}{
		{tags[0], fileCount * 2},
		{extraTag, 0},
		{tags[2], fileCount},
	}
	for _, condition := range conditions {
		count, err := CountFilesWithTag(db, condition.tag)
		if err != nil {
			t.Errorf("Could not count files: %s", err)
		}
		if count != condition.expectedCount {
			t.Errorf("Searching for %s yielded %d files but expected %d",
				condition.tag.Text, count, condition.expectedCount)
		}
	}
}

// Helper to create count files tagged with tagCount tags
func createFilesAndTags(db *sql.DB, baseName string, path string, fileCount int, tagCount int) ([]metadata.TagInfo, []metadata.FileInfo, error) {
	tags, err := createTags(db, "a", 3)
	if err != nil {
		return nil, nil, err
	}
	files := make([]metadata.FileInfo, fileCount)
	for i := 0; i < fileCount; i++ {
		files[i], err = CreateFileInPath(db, fmt.Sprintf("%s%d", baseName, i), path, tags[:tagCount])
		if err != nil {
			return nil, nil, err
		}
	}
	return tags, files, nil
}

// Helper to search a file list to see if a file exists
func isFileFound(files []metadata.FileInfo, searchFile metadata.FileInfo) bool {
	if files == nil {
		return false
	}
	for _, file := range files {
		if file.Id == searchFile.Id {
			return true
		}
	}
	return false
}

// Helper to create levels number of tags. If level is 1, only a top-level tag is created. For levels > 1, each tag
// will be associated to ALL of the other tags that preceded it.
func createTags(db *sql.DB, baseName string, levels int) ([]metadata.TagInfo, error) {
	var tags []metadata.TagInfo

	for i := 0; i < levels; i++ {
		tag, err := AddTag(db, fmt.Sprintf("%s%d", baseName, i), tags)
		if err != nil {
			return nil, err
		}
		if tag.Id == metadata.UnknownTag.Id {
			return nil, errors.New("could not save tag")
		}
		tags = append(tags, tag)
	}
	if len(tags) != levels {
		return nil, errors.New("could not create enough tags")
	}
	return tags, nil
}

// Helper to get a reference to an in-memory database. Callers should close the db when done.
func getDb(t *testing.T) *sql.DB {
	// need shared cache to allow different connections to use same in-memory db
	db, err := Open("file::memory:?cache=shared")
	if err != nil {
		t.Errorf("Could not open database")
	}
	return db
}
