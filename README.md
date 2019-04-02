Co-Occurring Tag File System (cotfs)
-----------------------------------

This is a simple FUSE file system that uses tags to organize files. When browsing the filesystem, tags are treated
as directories. Within a directory, any tag that co-occurs with the tag represented by the current directory are listed
as sub-directories. Files listed within a directory have ALL of the tags denoted by the path. The order of the tags 
does not matter.

For instance, a file with the following tags: photo, landscape, travel
could be accessed at any of the below paths:
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

Semantics

* mkdir - create tag
* rmdir - remove tag
* rm - removes the current tag (current directory) from the file
* cp - adds tags corresponding to destination path to file(s)
* mv - replaces tags corresponding to source path with those corresponding to destination path






Dependencies

* bazil.org/fuse
* github.com/mattn/go-sqlite3

NOTE: you need gcc installed when running "go install github.com/mattn/go-sqlite3"
