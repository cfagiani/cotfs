package cotfs

import (
	"bazil.org/fuse"
	"bazil.org/fuse/fs"
	"context"
	"database/sql"
	"github.com/cfagiani/cotfs/internal/pkg/db"
	"github.com/cfagiani/cotfs/internal/pkg/metadata"
	"io"
	"os"
)

// Mounts the filesystem at the path specified and opens a connection to the metadata database
func Mount(path, mountpoint string) error {
	database, err := db.Open(path)
	if err != nil {
		return err
	}
	defer database.Close()

	c, err := fuse.Mount(mountpoint)
	if err != nil {
		return err
	}
	defer c.Close()

	filesys := &FS{
		database: database,
	}
	if err := fs.Serve(c, filesys); err != nil {
		return err
	}

	// check if the mount process has an error to report
	<-c.Ready
	if err := c.MountError; err != nil {
		return err
	}

	return nil
}

type FS struct {
	database *sql.DB
}

var _ fs.FS = (*FS)(nil)

func (f *FS) Root() (fs.Node, error) {
	n := &Dir{
		database: f.database,
	}
	return n, nil
}

type Dir struct {
	database *sql.DB
	// nil for the root directory
	path []metadata.TagInfo
}

var _ fs.Node = (*Dir)(nil)

func tagAttr(a *fuse.Attr) {
	a.Size = 0
	a.Mode = os.ModeDir
}

func (d *Dir) Attr(ctx context.Context, a *fuse.Attr) error {
	if d.path == nil {
		// root directory
		a.Mode = os.ModeDir | 0755
		return nil
	}
	tagAttr(a)
	return nil
}

var _ = fs.NodeMkdirer(&Dir{})

func (d *Dir) Mkdir(ctx context.Context, req *fuse.MkdirRequest) (fs.Node, error) {
	tag, err := db.AddTag(d.database, req.Name, d.path)
	if err != nil {
		return nil, err
	}
	return &Dir{
		database: d.database,
		path:     appendIfNotFound(d.path, tag),
	}, nil
}

var _ = fs.NodeRequestLookuper(&Dir{})

func (d *Dir) Lookup(ctx context.Context, req *fuse.LookupRequest, resp *fuse.LookupResponse) (fs.Node, error) {

	var err error
	var foundTag metadata.TagInfo
	if d.path == nil || len(d.path) == 0 {
		foundTag, err = db.FindTag(d.database, req.Name)
		if err != nil {
			return nil, err
		}
	} else {
		//now we need to see if the name corresponds to a directory. We have to hit the db for that
		//doesn't matter which tag we use to check for co-incidence so just pick the first
		foundTag, err = db.GetCoincidentTag(d.database, req.Name, d.path[0].Text)
		if err != nil {
			return nil, err
		}
	}
	if foundTag.Id != metadata.UnknownTag.Id {
		//since we don't allow file listing in the root, we know this must be a directory
		return &Dir{
			database: d.database,
			path:     appendIfNotFound(d.path, foundTag),
		}, nil
	}
	info, _ := db.GetFilesWithTags(d.database, d.path, req.Name)
	if info != nil && len(info) > 0 {
		return &File{
			fileInfo: info[0],
		}, nil
	}
	return nil, fuse.ENOENT

}

var _ = fs.HandleReadDirAller(&Dir{})

func (d *Dir) ReadDirAll(ctx context.Context) ([]fuse.Dirent, error) {

	var res []fuse.Dirent

	tags, err := db.GetCoincidentTags(d.database, d.path)
	if err != nil {
		return nil, err
	}
	for _, tag := range tags {
		res = append(res, fuse.Dirent{Type: fuse.DT_Dir, Name: tag.Text})
	}

	// TODO: batch files in pseudo-directory if too many to list
	// for now, only list files if not in the root
	if d.path != nil && len(d.path) > 0 {
		files, fileError := db.GetFilesWithTags(d.database, d.path, "")
		if fileError != nil {
			return nil, fileError
		}
		for _, file := range files {
			res = append(res, fuse.Dirent{Name: file.Name, Type: fuse.DT_File})
		}
	}
	return res, nil
}

type File struct {
	fileInfo metadata.FileInfo
}

var _ fs.Node = (*File)(nil)

func (f *File) Attr(ctx context.Context, a *fuse.Attr) error {
	//zipAttr(f.file, a)
	return nil
}

var _ = fs.NodeOpener(&File{})

func (f *File) Open(ctx context.Context, req *fuse.OpenRequest, resp *fuse.OpenResponse) (fs.Handle, error) {
	r, err := os.Open(f.fileInfo.Path + "/" + f.fileInfo.Name)
	if err != nil {
		return nil, err
	}
	// individual entries inside a zip file are not seekable
	resp.Flags |= fuse.OpenNonSeekable
	return &FileHandle{r: r}, nil
}

type FileHandle struct {
	r io.ReadCloser
}

var _ fs.Handle = (*FileHandle)(nil)

var _ fs.HandleReleaser = (*FileHandle)(nil)

func (fh *FileHandle) Release(ctx context.Context, req *fuse.ReleaseRequest) error {
	return fh.r.Close()
}

var _ = fs.HandleReader(&FileHandle{})

func (fh *FileHandle) Read(ctx context.Context, req *fuse.ReadRequest, resp *fuse.ReadResponse) error {
	// We don't actually enforce Offset to match where previous read
	// ended. Maybe we should, but that would mean'd we need to track
	// it. The kernel *should* do it for us, based on the
	// fuse.OpenNonSeekable flag.
	//
	// One exception to the above is if we fail to fully populate a
	// page cache page; a read into page cache is always page aligned.
	// Make sure we never serve a partial read, to avoid that.
	buf := make([]byte, req.Size)
	n, err := io.ReadFull(fh.r, buf)
	if err == io.ErrUnexpectedEOF || err == io.EOF {
		err = nil
	}
	resp.Data = buf[:n]
	return err
}

func appendIfNotFound(tags []metadata.TagInfo, newTag metadata.TagInfo) []metadata.TagInfo {
	for _, tag := range tags {
		if tag.Text == newTag.Text {
			return tags
		}
	}

	//when we need to append, we want to ensure we have a new slice, so copy
	c := make([]metadata.TagInfo, len(tags)+1)
	copy(c, tags)
	//add the new entry
	c[len(c)-1] = newTag
	return c
}
