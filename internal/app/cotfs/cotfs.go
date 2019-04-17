package cotfs

import (
	"bazil.org/fuse"
	"bazil.org/fuse/fs"
	"context"
	"database/sql"
	"fmt"
	"github.com/cfagiani/cotfs/internal/pkg/db"
	"github.com/cfagiani/cotfs/internal/pkg/metadata"
	"io"
	"os"
	"strings"
	"syscall"
	"time"
)

var mountPoint string

// Mounts the filesystem at the path specified and opens a connection to the metadata database
func Mount(metadataPath string, mountpoint string) error {
	database, err := db.Open(metadataPath)
	mountPoint = mountpoint
	if err != nil {
		return err
	}
	defer database.Close()

	c, err := fuse.Mount(mountpoint,
		fuse.FSName("cotfs"),
		fuse.Subtype("cotfs"),
		fuse.LocalVolume(), //this only impacts Finder on MacOS
		fuse.VolumeName("Media Filesystem"),
	)
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
	a.Mode = os.ModeDir | 0755

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

var _ = fs.NodeSymlinker(&Dir{})

// Responds to symlink calls by adding the tags corresponding to the destination to the file specified by the target
// If the target of the link resides outside the cotfs file system, a new File database entry will be created pointing
// to the underlying file.
func (d *Dir) Symlink(ctx context.Context, req *fuse.SymlinkRequest) (fs.Node, error) {
	//no links in the root
	if d.path == nil {
		return nil, fuse.EPERM
	}
	absDirPath, fileName := convertToAbsolutePath(d.path, req.Target)
	if strings.Index(absDirPath, mountPoint) == 0 {
		return d.handleWithinFSLink(absDirPath, fileName)
	} else {
		// target is a real file outside our filesystem.
		return d.handleCrossDeviceLink(absDirPath, fileName)
	}
}

// Handles linking to a file that resides outside this cotfs file system. This function will find or create a new file
// record (only 1 file record per absolute path is permitted) and apply the tags from the destination directory to the
// file record.
func (d *Dir) handleCrossDeviceLink(absDirPath string, fileName string) (fs.Node, error) {
	// first make sure it is a file
	fi, err := os.Stat(fmt.Sprintf("%s%c%s", absDirPath, os.PathSeparator, fileName))
	if err != nil {
		return nil, err
	}
	if fi.Mode().IsDir() {
		// TODO: if target is a directory, recursively traverse it and add all the files,
		//  treating Intermediate subdirs as tags; for now, just return error
		return nil, fuse.EPERM
	}
	// See if the file already exists
	info, err := db.FindFileByAbsPath(d.database, fileName, absDirPath)
	if err != nil {
		return nil, err
	}
	if info.Id == metadata.UnknownFile.Id {
		// create the file record; we use the existing file name regardless of what the link specified
		info, err = db.CreateFileInPath(d.database, fileName, absDirPath, d.path)
		if err != nil {
			return nil, err
		}
	} else {
		// file already exists, just need to tag it
		err = db.TagFile(d.database, info.Id, d.path)
	}
	return &File{fileInfo: info}, err
}

// Handles creation of a link to a file that is already under management by cotfs by looking up the tags that correspond
// to the absoluteDirPath and applying the tags from the destination directory to the file.
// An error is returned if any of the tags in the path don't exist or the file doesn't exist.
func (d *Dir) handleWithinFSLink(absDirPath string, fileName string) (fs.Node, error) {
	// if we're within our mount point, then strip it off and convert to a set of TagInfos
	noMountPath := strings.Replace(absDirPath, mountPoint, "", 1)
	path, err := convertPathToTags(d.database, noMountPath)
	if err != nil {
		return nil, err
	}
	// now make sure the file exists
	files, err := db.GetFilesWithTags(d.database, path, fileName)
	if err != nil {
		return nil, err
	}
	if files == nil || len(files) == 0 {
		// file not found
		return nil, fuse.ENOENT
	} else if len(files) > 1 {
		// more than 1 file matches
		return nil, fuse.EPERM
	}
	// apply destination tags to the file
	err = db.TagFile(d.database, files[0].Id, d.path)
	if err != nil {
		return nil, err
	}
	return &File{fileInfo: files[0]}, nil
}

// Converts an absolute directory path to an array of tag info objects
func convertPathToTags(database *sql.DB, dirPath string) ([]metadata.TagInfo, error) {
	tokens := strings.Split(dirPath, string(os.PathSeparator))
	//build up a "path" array
	tags := make([]metadata.TagInfo, len(tokens))
	for i, tag := range tokens {
		var tagInfo metadata.TagInfo
		var err error
		if i == 0 {
			// if at the root, just lookup the tag
			tagInfo, err = db.GetTag(database, tag)
		} else {
			// otherwise, look for co-incident tag
			tagInfo, err = db.GetCoincidentTag(database, tag, tags[i-1].Text)
		}
		if err != nil {
			return nil, err
		}
		if tagInfo.Id == metadata.UnknownTag.Id {
			// not found return error
			return nil, fuse.ENOENT
		}
		tags[i] = tagInfo
	}
	return tags, nil
}

// Converts a path string to an absolute path, treating the path parameter as the current working directory (used when
// resolving relative paths).
func convertToAbsolutePath(path []metadata.TagInfo, newPath string) (string, string) {

	if strings.Index(newPath, string(os.PathSeparator)) == 0 {
		// already an absolute path
		lastSep := strings.LastIndex(newPath, string(os.PathSeparator))
		return newPath[0:lastSep], newPath[lastSep+1:]
	}
	cwd := make([]string, len(path))
	for i, t := range path {
		cwd[i] = t.Text
	}
	// get the absolute path to the working directory by combining with the mount point
	mountSplit := strings.Split(mountPoint, string(os.PathSeparator))
	if mountSplit[0] == "" {
		cwd = append(mountSplit[1:], cwd...)
	} else {
		cwd = append(mountSplit, cwd...)
	}

	// apply the tokens in the new path to the current working directory to get the final path
	tokens := strings.Split(newPath, string(os.PathSeparator))
	var fileName string
	for i, t := range tokens {
		if t == "." {
			continue
		}
		if i == len(tokens)-1 {
			fileName = t
		} else if t == ".." {
			cwd = cwd[:len(cwd)-1]
		} else {
			cwd = append(cwd, t)
		}
	}
	return fmt.Sprintf("%s%s", string(os.PathSeparator), strings.Join(cwd, string(os.PathSeparator))), fileName

}

var _ = fs.NodeLinker(&Dir{})

// Respond to hard link requests by applying the tags corresponding to the destination directory to the file.
// We only support linking to files and do not allow links in the root (as that would be an untagged file).
func (d *Dir) Link(ctx context.Context, req *fuse.LinkRequest, old fs.Node) (fs.Node, error) {
	//no links in the root
	if d.path == nil {
		return nil, fuse.EPERM
	}
	//ignore name, always use same name from existing file, just create a link by tagging
	switch node := old.(type) {
	case *Dir:
		return nil, fuse.EPERM
	case *File:
		err := db.TagFile(d.database, node.fileInfo.Id, d.path)
		if err != nil {
			return nil, err
		}
	}
	return old, nil
}

var _ = fs.NodeMkdirer(&Dir{})

// Respond to mkdir calls by creating a tag and linking it to the tags in the current path.
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

// Respond to rm by removing a tag (for removing directories) or un-tagging a file
func (d *Dir) Remove(ctx context.Context, req *fuse.RemoveRequest) error {
	if req.Dir {
		return d.handleTagRm(req)
	} else {
		return d.handleFileRm(req)
	}
}

// Disassociates a tag with its parent tag or, if at the root, removes the tag entirely. Removals will be rejected
// if the removal would leave any file un-tagged.
func (d *Dir) handleTagRm(req *fuse.RemoveRequest) error {
	// first get metadata corresponding to tag
	var dirTag metadata.TagInfo
	var err error
	if d.path != nil {
		dirTag, err = db.GetCoincidentTag(d.database, req.Name, d.path[0].Text)
	} else {
		dirTag, err = db.GetTag(d.database, req.Name)
	}

	if err != nil {
		return err
	}
	if dirTag.Id == metadata.UnknownTag.Id {
		return fuse.ENOENT
	}
	// if any files have ONLY this tag, refuse to remove because "not empty"
	count, err := db.GetFileCountWithSingleTag(d.database, dirTag)
	if err != nil {
		return err
	}
	if count > 0 {
		return error(syscall.ENOTEMPTY)
	}

	// remove tag from files with this particular set of tags (essentially pushing them "up" a directory)
	err = db.UntagFiles(d.database, appendIfNotFound(d.path, dirTag))
	if err != nil {
		return err
	}
	// remove tag_assoc record for parent if there is one
	if d.path != nil && len(d.path) > 0 {
		db.UnassociateTag(d.database, d.path[len(d.path)-1], dirTag)
	}
	// if no more files with tag present, remove tag
	count, err = db.CountFilesWithTag(d.database, dirTag)
	if err != nil {
		return err
	}
	if count == 0 {
		return db.DeleteTag(d.database, dirTag)
	}

	//TODO: is this the wrong error code? ENOTEMPTY shows up as IOError in MacOS
	return error(syscall.ENOTEMPTY)
}

// Removes a tag from a file.
func (d *Dir) handleFileRm(req *fuse.RemoveRequest) error {
	//if it's a file, just unlink from this tag
	files, err := db.GetFilesWithTags(d.database, d.path, req.Name)
	if err != nil {
		return err
	}
	if files == nil || len(files) == 0 {
		return fuse.ENOENT
	}
	for _, file := range files {
		err := db.UntagFile(d.database, file.Id, d.path[len(d.path)-1].Id)
		if err != nil {
			return err
		}
	}
	return nil
}

var _ = fs.NodeRequestLookuper(&Dir{})

// Looks up a single name within a directory. Names can be either a co-incident tag or a file.
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

// Lists all contents of a directory
func (d *Dir) ReadDirAll(ctx context.Context) ([]fuse.Dirent, error) {

	var res []fuse.Dirent

	tags, err := db.GetCoincidentTags(d.database, d.path, "")
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

	stat, err := os.Stat(fmt.Sprintf("%s%c%s", f.fileInfo.Path, os.PathSeparator, f.fileInfo.Name))
	if err != nil {
		return err
	}
	sysStat := stat.Sys().(*syscall.Stat_t)

	a.Size = uint64(stat.Size())
	a.Mode = stat.Mode()
	a.Mtime = stat.ModTime()
	a.Ctime = time.Unix(int64(sysStat.Ctimespec.Sec), int64(sysStat.Ctimespec.Nsec))
	a.Crtime = time.Unix(int64(sysStat.Ctimespec.Sec), int64(sysStat.Ctimespec.Nsec))

	return nil
}

var _ = fs.NodeOpener(&File{})

func (f *File) Open(ctx context.Context, req *fuse.OpenRequest, resp *fuse.OpenResponse) (fs.Handle, error) {

	r, err := os.Open(fmt.Sprintf("%s%c%s", f.fileInfo.Path, os.PathSeparator, f.fileInfo.Name))
	if err != nil {
		return nil, err
	}
	return &FileHandle{r: r}, nil
}

type FileHandle struct {
	r *os.File
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
	return append(tags, newTag)
}
