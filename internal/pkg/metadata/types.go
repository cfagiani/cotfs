package metadata

type FileInfo struct {
	Id       uint32
	Name     string
	Path     string
	Created  uint32
	Modified uint32
	BackedUp bool
}
