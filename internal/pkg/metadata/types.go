package metadata

type FileInfo struct {
	Id       int64
	Name     string
	Path     string
	Created  uint32
	Modified uint32
	BackedUp bool
}

type TagInfo struct {
	Id   int64
	Text string
}

var UnknownTag = TagInfo{Id: -1, Text: ""}
