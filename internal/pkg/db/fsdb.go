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
	"CREATE TABLE IF NOT EXISTS file(id INTEGER PRIMARY KEY, name text, path text, created INTEGER, modified INTEGER, backed_up INTEGER);",
	"CREATE TABLE IF NOT EXISTS file_tags(id INTEGER PRIMARY KEY, fid INTEGER, tid INTEGER);",
	"CREATE TABLE IF NOT EXISTS tag_assoc(id, INTEGER PRIMARY KEY, t1 INTEGER, t2 INTEGER);",
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
func GetAllTags(db *sql.DB) ([]string, error) {
	rows, err := db.Query("select txt from tag order by txt DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []string
	for rows.Next() {
		var tag string
		err = rows.Scan(&tag)
		if err != nil {
			return nil, err
		}
		results = append(results, tag)
	}
	return results, nil
}

//Returns true if tag exists
func FindTag(db *sql.DB, tag string) (bool, error) {
	query := "select txt from tag where tag.txt = ?"
	stmt, err := db.Prepare(query)
	if err != nil {
		return false, err
	}
	defer stmt.Close()
	rows, err := stmt.Query(tag)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	if rows.Next() {
		return true, nil
	} else {
		return false, nil
	}
}

//Returns true if the two tags passed in co-occur on some file.
func IsTagCoincident(db *sql.DB, tagOne string, tagTwo string) (bool, error) {
	query := "select txt from tag where tag.txt = ? and tag.id in " +
		" (select ta.t1 from tag_assoc ta, tag tt where tt.txt = ? and tt.id = ta.t2 " +
		" UNION select ta.t2 from tag_assoc ta, tag tt where tt.txt = ? and tt.id = ta.t1 )"
	stmt, err := db.Prepare(query)
	if err != nil {
		return false, err
	}
	defer stmt.Close()
	rows, err := stmt.Query(tagOne, tagTwo, tagTwo)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	if rows.Next() {
		return true, nil
	} else {
		return false, nil
	}
}

//Lists all the tags that co-occur with ALL the tags passed in.
func GetCoincidentTags(db *sql.DB, tags []string) ([]string, error) {
	if tags == nil || len(tags) == 0 {
		return GetAllTags(db)
	}
	//need this because of the way go handles variadic parameters with the empty interface
	var params []interface{} = make([]interface{}, len(tags)*2)
	j := 0
	query := "SELECT DISTINCT ot.txt FROM tag ot WHERE ot.id in ("
	for i := 0; i < len(tags); i++ {
		if i > 0 {
			query += " INTERSECT "
		}
		query += " select * from ( select ta.t1 from tag_assoc ta, tag t where t.id = ta.t2 and t.txt = ? UNION select ta.t2 from tag_assoc ta, tag t where t.id = ta.t1 and t.txt = ? )"
		params[j] = tags[i]
		j += 1
		params[j] = tags[i]
		j += 1
	}
	query += ") ORDER BY ot.txt DESC"

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
	var results []string
	for rows.Next() {
		var tag string
		err = rows.Scan(&tag)
		if err != nil {
			return nil, err
		}
		results = append(results, tag)
	}
	return results, nil
}

//Lists the files that have ALL the tags passed in, optionally filtered by name (if name has a length of > 0)
//Name can also contain 0 or more wildcards characters (*).
func GetFilesWithTags(db *sql.DB, tags []string, name string) ([]metadata.FileInfo, error) {
	//need this because of the way go handles variadic parameters with the empty interface
	paramLength := len(tags)
	if len(name) > 0 {
		paramLength += 1
	}
	var params []interface{} = make([]interface{}, paramLength)
	query := "SELECT f.id, f.name, f.path, f.created, f.modified, f.backed_up from file f where EXISTS "
	for i := 0; i < len(tags); i++ {
		if i > 0 {
			query += " AND EXISTS "
		}
		query += "(SELECT 1 FROM file_tags ft, tag t WHERE ft.tid = t.id and fid = f.id AND t.txt = ?)"
		params[i] = tags[i]
	}
	if len(name) > 0 {
		operator := " = "
		if strings.Index(name, "*") >= 0 {
			operator = " like "
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
		err = rows.Scan(&info.Id, &info.Name, &info.Path, &info.Created, &info.Modified, &info.BackedUp)
		if err != nil {
			return nil, err
		}
		results = append(results, info)
	}
	return results, nil
}
