package db

import (
	"database/sql"
	"fmt"
	"github.com/cfagiani/cotfs/internal/pkg/metadata"
	_ "github.com/mattn/go-sqlite3"
	"log"
	"strings"
)

var ddl = []string{
	"CREATE TABLE IF NOT EXISTS tag(id INTEGER PRIMARY KEY, txt text);",
	"CREATE TABLE IF NOT EXISTS file_md(id INTEGER PRIMARY KEY, name text, path text);",
	"CREATE TABLE IF NOT EXISTS file_tags(fid INTEGER, tid INTEGER, PRIMARY KEY (fid,tid));",
	"CREATE TABLE IF NOT EXISTS tag_assoc(t1 INTEGER, t2 INTEGER, PRIMARY KEY (t1,t2));",
	"CREATE UNIQUE INDEX IF NOT EXISTS tag_idx ON tag(txt);"}

//Opens the database and creates the schema if it is not present.
func Open(filename string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", filename)
	if err != nil {
		log.Fatal(err)
	}
	for i := 0; i < len(ddl); i++ {
		_, err = db.Exec(ddl[i])
		if err != nil {
			log.Printf("%q: %s\n", err, ddl[i])
			return nil, err
		}
	}
	return db, nil
}

//Lists all tags in the database.
func GetAllTags(db *sql.DB) ([]metadata.TagInfo, error) {
	rows, err := db.Query("select id, txt from tag order by txt DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []metadata.TagInfo
	for rows.Next() {
		var tag = metadata.TagInfo{}
		err = rows.Scan(&tag.Id, &tag.Text)
		if err != nil {
			return nil, err
		}
		results = append(results, tag)
	}
	return results, nil
}

// Removes the assoc record between the two tags
func UnassociateTag(db *sql.DB, tagOne metadata.TagInfo, tagTwo metadata.TagInfo) error {
	_, err := db.Exec("DELETE FROM tag_assoc where t1 = ? and t2 = ?", min(tagOne.Id, tagTwo.Id), max(tagOne.Id, tagTwo.Id))
	return err
}

// Deletes a tag from the tag and tag_assoc table
func DeleteTag(db *sql.DB, tag metadata.TagInfo) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	_, err = db.Exec("DELETE FROM TAG_ASSOC WHERE t1 = ? or t2 = ?", tag.Id, tag.Id)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	_, err = db.Exec("DELETE FROM TAG WHERE id = ?", tag.Id)
	return tx.Commit()
}

// Adds a tag to the database and updates the co-occurrence table.
// If the tag already exists, only the co-occurrence table will be updated.
// Returns id of tag
func AddTag(db *sql.DB, newTag string, tagContext []metadata.TagInfo) (metadata.TagInfo, error) {
	existingTag, err := FindTag(db, newTag)
	if err != nil {
		return metadata.UnknownTag, err
	}
	tx, err := db.Begin()

	if err != nil {
		_ = tx.Rollback()
		return metadata.UnknownTag, err

	}
	if existingTag.Id < 0 {
		//tag does not exist, need to insert
		res, err := db.Exec("INSERT INTO tag (txt) VALUES(?)", newTag)
		if err != nil {
			_ = tx.Rollback()
			return metadata.UnknownTag, err
		}
		newId, err := res.LastInsertId()
		if err != nil {
			_ = tx.Rollback()
			return metadata.UnknownTag, err
		}
		existingTag = metadata.TagInfo{Id: newId, Text: newTag}
	}
	//now update co-incidence table
	//we enforce that t1 < t2 and ignore conflicts so we don't have to do checking on rows
	if tagContext != nil {
		for _, tag := range tagContext {
			_, err = db.Exec("INSERT OR IGNORE INTO tag_assoc VALUES (?,?)",
				min(tag.Id, existingTag.Id), max(tag.Id, existingTag.Id))
			if err != nil {
				_ = tx.Rollback()
				return existingTag, err
			}
		}
	}
	err = tx.Commit()
	if err != nil {
		return existingTag, err
	}
	return existingTag, nil
}

// Gets the id of a tag by name. If no tag exists, returns metadata.UnknownTag
func FindTag(db *sql.DB, tag string) (metadata.TagInfo, error) {
	query := "select id, txt from tag where tag.txt = ?"
	stmt, err := db.Prepare(query)
	if err != nil {
		return metadata.UnknownTag, err
	}
	defer stmt.Close()
	rows, err := stmt.Query(tag)
	if err != nil {
		return metadata.UnknownTag, err
	}
	defer rows.Close()
	if rows.Next() {
		info := metadata.TagInfo{}
		err := rows.Scan(&info.Id, &info.Text)
		if err != nil {
			return metadata.UnknownTag, err
		} else {
			return info, nil
		}
	} else {
		return metadata.UnknownTag, nil
	}
}

// Returns tag record for tagOne if it is co-incident with tagTwo.
func GetCoincidentTag(db *sql.DB, tagOne string, tagTwo string) (metadata.TagInfo, error) {
	query := "select id, txt from tag where tag.txt = ? and tag.id in " +
		" (select ta.t1 from tag_assoc ta, tag tt where tt.txt = ? and tt.id = ta.t2 " +
		" UNION select ta.t2 from tag_assoc ta, tag tt where tt.txt = ? and tt.id = ta.t1 )"
	stmt, err := db.Prepare(query)
	if err != nil {
		return metadata.UnknownTag, err
	}
	defer stmt.Close()
	rows, err := stmt.Query(tagOne, tagTwo, tagTwo)
	if err != nil {
		return metadata.UnknownTag, err
	}
	defer rows.Close()
	if rows.Next() {
		var tagInfo = metadata.TagInfo{}
		err = rows.Scan(&tagInfo.Id, &tagInfo.Text)
		if err != nil {
			return metadata.UnknownTag, err
		}
		return tagInfo, nil
	} else {
		return metadata.UnknownTag, nil
	}
}

// Looks up a single tag in the database by name (text)
func GetTag(db *sql.DB, name string) (metadata.TagInfo, error) {
	stmt, err := db.Prepare("select id, txt from tag where txt = ?")
	if err != nil {
		return metadata.UnknownTag, err
	}
	defer stmt.Close()
	rows, err := stmt.Query(name)
	if err != nil {
		return metadata.UnknownTag, err
	}
	defer rows.Close()
	if rows.Next() {
		var tag = metadata.TagInfo{}
		err = rows.Scan(&tag.Id, &tag.Text)
		if err != nil {
			return metadata.UnknownTag, err
		}
		return tag, nil
	}
	return metadata.UnknownTag, nil

}

// Lists all the tags that co-occur with ALL the tags passed in, optionally filtered by name
func GetCoincidentTags(db *sql.DB, tags []metadata.TagInfo, name string) ([]metadata.TagInfo, error) {
	if tags == nil || len(tags) == 0 {
		return GetAllTags(db)
	}
	// need this because of the way go handles variadic parameters with the empty interface
	paramSize := len(tags) * 2
	if len(name) > 0 {
		paramSize++
	}
	var params = make([]interface{}, paramSize)
	j := 0
	query := "SELECT DISTINCT ot.Id, ot.txt FROM tag ot WHERE ot.id in ("
	for i := 0; i < len(tags); i++ {
		if i > 0 {
			query += " INTERSECT "
		}
		query += " select * from ( select ta.t1 from tag_assoc ta, tag t where t.id = ta.t2 and t.txt = ? UNION select ta.t2 from tag_assoc ta, tag t where t.id = ta.t1 and t.txt = ? )"
		params[j] = tags[i].Text
		j += 1
		params[j] = tags[i].Text
		j += 1
	}
	query += ") "
	if len(name) > 0 {
		operator := " = "
		if strings.Index(name, "*") >= 0 {
			operator = " LIKE "
		}
		params[paramSize-1] = strings.Replace(name, "*", "%", -1)
		query += fmt.Sprintf(" AND ot.txt %s ?", operator)
	}
	query += " ORDER BY ot.txt ASC"

	stmt, err := db.Prepare(query)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	rows, err := stmt.Query(params...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []metadata.TagInfo
	for rows.Next() {
		var info = metadata.TagInfo{}
		err = rows.Scan(&info.Id, &info.Text)
		if err != nil {
			return nil, err
		}
		results = append(results, info)
	}
	return results, nil
}

// Applies all the tags passed in to a file, if they don't already exist
func TagFile(db *sql.DB, fileId int64, tags []metadata.TagInfo) error {
	if tags == nil || len(tags) == 0 {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	for _, tag := range tags {
		_, err = db.Exec("INSERT OR IGNORE INTO file_tags VALUES(?,?)", fileId, tag.Id)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// Removes a tag from a file identified by file id
func UntagFile(db *sql.DB, fileId int64, tagId int64) error {
	_, err := db.Exec("DELETE FROM file_tags WHERE fid = ? AND tid = ?", fileId, tagId)
	// TODO: should we remove the File record if it has no more tags?
	return err
}

// Removes the tag corresponding to the last entry in the path passed in from all files in that path.
func UntagFiles(db *sql.DB, path []metadata.TagInfo) error {
	files, err := GetFilesWithTags(db, path, "")
	if err != nil {
		return err
	}
	if files != nil && len(files) > 0 {
		tx, err := db.Begin()
		if err != nil {
			_ = tx.Rollback()
			return err

		}
		for _, file := range files {
			_, err := db.Exec("DELETE FROM FILE_TAGS WHERE FID = ? AND TID = ?", file.Id, path[len(path)-1].Id)
			if err != nil {
				_ = tx.Rollback()
				return err
			}
		}
		return tx.Commit()
	}
	return nil
}

// Looks up a file using the name and absolute path in the underlying filesystem (not the tag path). Returns UnknownFile
// if not found.
func FindFileByAbsPath(db *sql.DB, name string, absPath string) (metadata.FileInfo, error) {
	stmt, err := db.Prepare("SELECT id, name, path FROM file_md WHERE name = ? AND path = ?")
	if err != nil {
		return metadata.UnknownFile, err
	}
	defer stmt.Close()
	rows, err := stmt.Query(name, absPath)
	if err != nil {
		return metadata.UnknownFile, err
	}
	defer rows.Close()
	if rows.Next() {
		info := metadata.FileInfo{}
		err = rows.Scan(&info.Id, &info.Name, &info.Path)
		if err != nil {
			return metadata.UnknownFile, err
		}
		return info, nil
	}
	return metadata.UnknownFile, nil
}

// Creates a file record using the name and absolute path passed in and tags it with all the tags in the tagPath array.
func CreateFileInPath(db *sql.DB, name string, absPath string, tagPath []metadata.TagInfo) (metadata.FileInfo, error) {
	tx, err := db.Begin()
	if err != nil {
		return metadata.UnknownFile, err
	}
	res, err := db.Exec("INSERT INTO file_md (NAME, PATH) VALUES (?, ?)", name, absPath)
	if err != nil {
		_ = tx.Rollback()
		return metadata.UnknownFile, err
	}
	newId, err := res.LastInsertId()
	if err != nil {
		_ = tx.Rollback()
		return metadata.UnknownFile, err
	}
	fileInfo := metadata.FileInfo{Id: newId, Path: absPath, Name: name}
	// now tag it
	for _, tag := range tagPath {
		_, err := db.Exec("INSERT INTO FILE_TAGS (fid, tid) VALUES (?,?)", newId, tag.Id)
		if err != nil {
			_ = tx.Rollback()
			return metadata.UnknownFile, err
		}
	}
	return fileInfo, tx.Commit()
}

// Gets files tagged with only the tag specified.
func GetFileCountWithSingleTag(db *sql.DB, tag metadata.TagInfo) (int, error) {
	stmt, err := db.Prepare("select count(*) from (select 1 from file_tags where fid in (select fid from file_tags where tid = ?) group by fid having count(*)  = 1)")
	if err != nil {
		return -1, err
	}
	defer stmt.Close()
	rows, err := stmt.Query(tag.Id)
	if err != nil {
		return -1, err
	}
	defer rows.Close()
	if rows.Next() {
		var cnt int
		err = rows.Scan(&cnt)
		return cnt, nil
	}
	return 0, nil
}

// Counts number of files tagged with the tag passed in.
func CountFilesWithTag(db *sql.DB, tag metadata.TagInfo) (int, error) {
	stmt, err := db.Prepare("SELECT count(*) FROM file_tags WHERE tid = ?")
	if err != nil {
		return -1, err
	}
	defer stmt.Close()
	rows, err := stmt.Query(tag.Id)
	if err != nil {
		return -1, err
	}
	defer rows.Close()
	if rows.Next() {
		var count int
		err = rows.Scan(&count)
		if err != nil {
			return -1, err
		}
		return count, nil
	} else {
		return 0, nil
	}
}

// Lists the files that have ALL the tags passed in, optionally filtered by name (if name has a length of > 0)
// Name can also contain 0 or more wildcards characters (*).
func GetFilesWithTags(db *sql.DB, tags []metadata.TagInfo, name string) ([]metadata.FileInfo, error) {
	//need this because of the way go handles variadic parameters with the empty interface
	paramLength := len(tags)
	if len(name) > 0 {
		paramLength += 1
	}
	var params = make([]interface{}, paramLength)
	query := "SELECT f.id, f.name, f.path from file_md f where EXISTS "
	for i := 0; i < len(tags); i++ {
		if i > 0 {
			query += " AND EXISTS "
		}
		query += "(SELECT 1 FROM file_tags ft, tag t WHERE ft.tid = t.id and fid = f.id AND t.txt = ?)"
		params[i] = tags[i].Text
	}
	if len(name) > 0 {
		operator := " = "
		if strings.Index(name, "*") >= 0 {
			operator = " LIKE "
		}
		params[len(tags)] = strings.Replace(name, "*", "%", -1)
		query += fmt.Sprintf(" AND f.name %s ?", operator)
	}

	stmt, err := db.Prepare(query)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	rows, err := stmt.Query(params...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []metadata.FileInfo
	for rows.Next() {
		info := metadata.FileInfo{}
		err = rows.Scan(&info.Id, &info.Name, &info.Path)
		if err != nil {
			return nil, err
		}
		results = append(results, info)
	}
	return results, nil
}

func min(a int64, b int64) int64 {
	if a <= b {
		return a
	} else {
		return b
	}
}

func max(a int64, b int64) int64 {
	if a >= b {
		return a
	} else {
		return b
	}
}
