Co-Occurring Tag File System (cotfs)
-----------------------------------

# Overview
This is a simple FUSE file system that uses tags to organize files. When browsing the filesystem, tags are treated
as directories. Within a directory, any tag that co-occurs with the tag represented by the current directory are listed
as sub-directories. Files listed within a directory have ALL of the tags denoted by the path. The order of the tags 
does not matter.

For instance, a file with the following tags: photo, landscape, travel
could be accessed at any of the below paths:
```
/photo/landscape/travel/
/photo/travel/landscape/
/travel/photo/landscape
/travel/landscape/photo/
/landscape/travel/photo
/landscape/photo/travel/
/landscape/photo
/landscape/travel
/photo/landscape
/photo/travel
/travel/photo
/travel/landscape
/travel
/photo
/landscape
```

### Semantics

This filesystem is metadata-only. You cannot directly create a file in the filesystem. Instead, create your file(s) 
elsewhere and create links in the desired tag-based directory structure.

* mkdir - create tag
* rmdir - remove tag
* rm - removes the current tag (current directory) from the file
* ln - Applies all the tags corresponding to the destination directory to the file in the target. If the target lies 
outside the cotfs filesystem, a new record will be created  

NOTE: mv and cp are not supported.

## Prerequisites
Go 1.9+

## Dependencies

* bazil.org/fuse
* github.com/mattn/go-sqlite3

NOTE: you need gcc installed when running "go install github.com/mattn/go-sqlite3"


## Possible Enhancements
* support for indexing remote filesystems (google drive/photos, dropbox, s3)
* pre-create "top-level" tags to keep directory listing resonable? Other alternative would be some sort of grouping psuedo-tag(s)
