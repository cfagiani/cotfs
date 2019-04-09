package metadata

type FileInfo struct {
	Id   int64
	Name string
	Path string
}

type TagInfo struct {
	Id   int64
	Text string
}

var UnknownTag = TagInfo{Id: -1, Text: ""}

var UnknownFile = FileInfo{Id: -1}
