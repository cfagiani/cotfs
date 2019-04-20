package indexer

import (
	"database/sql"
	"github.com/cfagiani/cotfs/internal/pkg/db"
	"github.com/cfagiani/cotfs/internal/pkg/metadata"
	"log"
	"os"
	"path/filepath"
	"strings"
)

var defaultTag = "uncategorized"

//TODO: externalize into configuration file
var extensionToTagMap = map[string][]string{
	".jpg":     {"media", "image"},
	".jpeg":    {"media", "image"},
	".bmp":     {"media", "image"},
	".png":     {"media", "image"},
	".gif":     {"media", "image"},
	".tiff":    {"media", "image"},
	".tif":     {"media", "image"},
	".ico":     {"media", "image"},
	".svg":     {"media", "image"},
	".psd":     {"media", "image"},
	".odt":     {"document"},
	".rtf":     {"document"},
	".doc":     {"document"},
	".docx":    {"document"},
	".pages":   {"document"},
	".md":      {"document"},
	".ps":      {"document"},
	".eml":     {"document", "email"},
	".ppt":     {"document", "presentation"},
	".pptx":    {"document", "presentation"},
	".key":     {"document", "presentation"},
	".xls":     {"document", "spreadsheet"},
	".xlsx":    {"document", "spreadsheet"},
	".xlsm":    {"document", "spreadsheet"},
	".csv":     {"document", "spreadsheet"},
	".numbers": {"document", "spreadsheet"},
	".ods":     {"document", "spreadsheet"},
	".txt":     {"document"},
	".pdf":     {"document"},
	".mp3":     {"media", "audio"},
	".wav":     {"media", "audio"},
	".wma":     {"media", "audio"},
	".cda":     {"media", "audio"},
	".mov":     {"media", "video"},
	".wmv":     {"media", "video"},
	".mp4":     {"media", "video"},
	".avi":     {"media", "video"},
	".flv":     {"media", "video"},
	".h264":    {"media", "video"},
	".mpg":     {"media", "video"},
	".mpeg":    {"media", "video"},
	".zip":     {"archive"},
	".tar":     {"archive"},
	".gz":      {"archive"},
	".tgz":     {"archive"},
	".7z":      {"archive"},
	".rar":     {"archive"},
	".dmg":     {"archive"},
	".java":    {"code", "java"},
	".xml":     {"code", "xml"},
	".css":     {"code", "css", "web"},
	".html":    {"code", "html", "web"},
	".htm":     {"code", "html", "web"},
	".sh":      {"code", "scripts"},
	".py":      {"code", "python"},
	".go":      {"code", "go"},
	".sql":     {"code", "sql"},
	".json":    {"code", "javascript"},
	".js":      {"code", "javascript", "web"},
}

// Indexes a single path and adds any files found to the filesystem metadata database.
func IndexPath(pathToIndex string, metadataPath string) error {
	database, err := db.Open(metadataPath)
	if err != nil {
		return err
	}
	defer database.Close()
	tagCache := initTagCache(database, extensionToTagMap)
	//TODO if we support other types of paths (i.e. google, s3, etc) figure out the scheme and call right func here
	return indexLocalDirectory(database, pathToIndex, tagCache)
}

// Indexes a single local directory (recursively). Any files discovered will be added to the metadata database.
func indexLocalDirectory(database *sql.DB, pathToIndex string, tagCache map[string][]metadata.TagInfo) error {
	return filepath.Walk(pathToIndex, func(path string, info os.FileInfo, err error) error {
		// we only care about files for now
		if info.IsDir() {
			//TODO maybe create tags for some of the subdirs?
			return nil
		}
		// first see if the file is already in the database
		existingFile, _ := db.FindFileByAbsPath(database, filepath.Base(path), filepath.Dir(path))
		if existingFile.Id == metadata.UnknownFile.Id {
			// get count of files with that name
			tags := inferTagsFromFile(path, tagCache)
			_, err := db.CreateFileInPath(database, filepath.Base(path), filepath.Dir(path), tags)
			if err != nil {
				log.Printf("Could not add file %s", err)
			}
		}
		return nil
	})
}

// Converts the tag names in the tagsToMap map to TagInfo objects by looking them up in the DB.
func initTagCache(database *sql.DB, tagsToMap map[string][]string) map[string][]metadata.TagInfo {
	tagCache := make(map[string][]metadata.TagInfo)
	for key, val := range tagsToMap {
		tags := make([]metadata.TagInfo, len(val))
		for i, tagName := range val {
			// db already supports returning existing tag if it already exists so we can just call Add blindly
			tags[i], _ = db.AddTag(database, tagName, tags)
		}
		tagCache[key] = tags
	}
	defaultInfo, _ := db.AddTag(database, defaultTag, nil)
	tagCache[defaultTag] = []metadata.TagInfo{defaultInfo}
	return tagCache
}

// Infers tags to attribute to a file based on its name/path. Uses the tagCache passed in to map file extensions to
// a set of TagInfo objects that should be used.
func inferTagsFromFile(path string, tagCache map[string][]metadata.TagInfo) []metadata.TagInfo {
	extension := strings.ToLower(filepath.Ext(path))
	if val, ok := tagCache[extension]; ok {
		return val
	} else {
		return tagCache[defaultTag]
	}
}
