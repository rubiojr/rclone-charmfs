package charm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path/filepath"
	"time"

	iofs "io/fs"

	"github.com/charmbracelet/charm/client"
	"github.com/charmbracelet/charm/crypt"
	charmfs "github.com/charmbracelet/charm/fs"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config/configmap"
	"github.com/rclone/rclone/fs/config/configstruct"
	"github.com/rclone/rclone/fs/hash"
	"golang.org/x/sync/errgroup"
)

var (
	timeUnset = time.Unix(0, 0)
	// Check the interfaces are satisfied
	_ fs.Fs          = &Fs{}
	_ fs.PutStreamer = &Fs{}
	_ fs.Object      = &Object{}
	_ fs.MimeTyper   = &Object{}
)

var (
	errNotSupported = errors.New("not supported")
)

func init() {
	fsi := &fs.RegInfo{
		Name:        "charm",
		Description: "Charm Server FS",
		NewFs:       NewFs,
		Options: []fs.Option{{
			Name:     "url",
			Help:     "URL of your Charm server.",
			Required: false,
		}},
	}
	fs.Register(fsi)
}

// Options defines the configuration for this backend
type Options struct {
	Endpoint string `config:"url"`
}

// Fs stores the interface to the remote CharmFS files
type Fs struct {
	name        string
	root        string
	features    *fs.Features   // optional features
	opt         Options        // options for this backend
	ci          *fs.ConfigInfo // global config
	cfs         *charmfs.FS
	charmClient *client.Client
	crypt       *crypt.Crypt
	endpoint    *url.URL
	ctx         context.Context
}

// Object is a remote object that has been stat'd
type Object struct {
	fs          *Fs
	remote      string
	modTime     time.Time
	contentType string
	info        iofs.FileInfo
}

// NewFs creates a new Fs object from the name and root. It connects to
// the host specified in the config file.
func NewFs(ctx context.Context, name, root string, m configmap.Mapper) (fs.Fs, error) {
	// Parse config into Options struct
	opt := new(Options)
	err := configstruct.Set(m, opt)
	if err != nil {
		return nil, err
	}

	// Parse the endpoint and stick the root onto it
	base, err := url.Parse(opt.Endpoint)
	if err != nil {
		return nil, err
	}

	charmClient, err := client.NewClientWithDefaults()
	if err != nil {
		return nil, err
	}

	cfs, err := charmfs.NewFSWithClient(charmClient)
	if err != nil {
		return nil, err
	}

	crypt, err := crypt.NewCrypt()
	if err != nil {
		return nil, err
	}

	ci := fs.GetConfig(ctx)
	ci.IgnoreSize = true

	f := &Fs{
		name:        name,
		root:        root,
		opt:         *opt,
		ctx:         ctx,
		ci:          ci,
		cfs:         cfs,
		crypt:       crypt,
		endpoint:    base,
		charmClient: charmClient,
	}
	f.features = (&fs.Features{
		CanHaveEmptyDirectories: true,
	}).Fill(ctx, f)

	o, err := f.NewObject(ctx, root)
	if err != nil {
		return f, err
	}

	if o.(*Object).info == nil {
		nr := filepath.Dir(root)
		if nr == "." {
			nr = ""
		}
		f.root = nr
		return f, nil
	}

	if !o.(*Object).info.IsDir() {
		nr := filepath.Dir(root)
		if nr == "." {
			nr = ""
		}
		f.root = nr
		fs.Infof(f, "root is file, new root: %s", f.root)
		return f, fs.ErrorIsFile
	}

	return f, nil
}

func (f *Fs) Name() string {
	return f.name
}

func (f *Fs) Root() string {
	return f.root
}

func (f *Fs) String() string {
	return f.endpoint.String()
}

func (f *Fs) Features() *fs.Features {
	return f.features
}

func (f *Fs) Precision() time.Duration {
	return time.Second
}

func (f *Fs) NewObject(ctx context.Context, remote string) (fs.Object, error) {
	o := &Object{
		fs:     f,
		remote: remote,
	}

	return o, o.stat(ctx)
}

func (f *Fs) List(ctx context.Context, dir string) (entries fs.DirEntries, err error) {
	centries, err := f.cfs.ReadDir(filepath.Join(f.root, dir))
	if err != nil {
		return nil, err
	}
	// Charm returns an empty list without error if the directory does
	// not exist
	// https://github.com/charmbracelet/charm/blob/9d0f28b6e656e8b22170a3ab12f5121d7c72b8ea/fs/fs.go#L289
	if len(centries) == 0 {
		return nil, fs.ErrorDirNotFound
	}

	for _, ce := range centries {
		var entry fs.DirEntry
		path := ce.Name()
		df := filepath.Join(dir, path)
		if ce.IsDir() {
			entry = fs.NewDir(df, timeUnset)
		} else {
			entry = &Object{
				fs:     f,
				remote: df,
			}
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

// Put in to the remote path with the modTime given of the given size
//
// May create the object even if it returns an error - if so
// will return the object and the error, otherwise will return
// nil and the error
func (f *Fs) Put(ctx context.Context, in io.Reader, src fs.ObjectInfo, options ...fs.OpenOption) (fs.Object, error) {
	// Temporary object under construction
	o := &Object{
		fs:     f,
		remote: src.Remote(),
	}
	err := o.Update(ctx, in, src, options...)
	if err != nil {
		return nil, err
	}
	return o, nil
}

// PutStream uploads to the remote path with the modTime given of indeterminate size
func (f *Fs) PutStream(ctx context.Context, in io.Reader, src fs.ObjectInfo, options ...fs.OpenOption) (fs.Object, error) {
	return nil, errNotSupported
}

// Fs is the filesystem this remote file object is located within
func (o *Object) Fs() fs.Info {
	return o.fs
}

// String returns the URL to the remote HTTP file
func (o *Object) String() string {
	if o == nil {
		return "<nil>"
	}
	return o.remote
}

// Remote the name of the remote HTTP file, relative to the fs root
func (o *Object) Remote() string {

	return o.remote
}

// Hash returns "" since HTTP (in Go or OpenSSH) doesn't support remote calculation of hashes
func (o *Object) Hash(ctx context.Context, r hash.Type) (string, error) {
	return "", hash.ErrUnsupported
}

// Size returns the remote object size
//
// Files are encrypted in CharmFS so it's not possible to compare
// source object size (plaintext) with destination object file (ciphertext)
// without reading the remote object and decrypting it first.
func (o *Object) Size() int64 {
	if o.info == nil {
		return -1
	}
	return o.info.Size()
}

// ModTime returns the modification time of the remote file
func (o *Object) ModTime(ctx context.Context) time.Time {
	return o.modTime
}

// stat updates the info field in the Object
func (o *Object) stat(ctx context.Context) error {
	fs.Infof(o, "stat root: %s, remote: %s", o.fs.root, o.remote)
	ro, err := o.fs.cfs.Open(o.fs.root)
	if err != nil {
		return err
	}

	o.info, err = ro.Stat()
	if err != nil {
		return err
	}

	return nil
}

// SetModTime sets the modification and access time to the specified time
//
// it also updates the info field
func (o *Object) SetModTime(ctx context.Context, modTime time.Time) error {
	return errNotSupported
}

// Storable returns whether the remote file is a regular file (not a directory, symbolic link, block device, character device, named pipe, etc.)
func (o *Object) Storable() bool {
	return true
}

// Open a remote file object for reading. Seek is supported
func (o *Object) Open(ctx context.Context, options ...fs.OpenOption) (in io.ReadCloser, err error) {

	in, err = o.fs.cfs.Open(o.remote)
	if err != nil {
		return nil, err
	}

	return in, err
}

// Hashes returns hash.HashNone to indicate remote hashing is unavailable
func (f *Fs) Hashes() hash.Set {
	return hash.Set(hash.None)
}

// Mkdir makes the root directory of the Fs object
func (f *Fs) Mkdir(ctx context.Context, dir string) error {
	return errNotSupported
}

// Remove a remote file object
func (o *Object) Remove(ctx context.Context) error {
	opath := filepath.Join(o.fs.root, o.remote)
	fs.Infof(o, "removing object %s", opath)
	return o.fs.cfs.Remove(opath)
}

// Rmdir removes the root directory of the Fs object
func (f *Fs) Rmdir(ctx context.Context, dir string) error {
	centries, err := f.cfs.ReadDir(dir)
	if err != nil {
		return err
	}

	if len(centries) > 0 {
		return fs.ErrorDirectoryNotEmpty
	}

	return nil
}

// Update in to the object with the modTime given of the given size
func (o *Object) Update(ctx context.Context, in io.Reader, src fs.ObjectInfo, options ...fs.OpenOption) error {
	fs.Infof(o, "update remote: %s, root: %s ", o.remote, o.fs.root)
	err := o.fs.writeFile(ctx, filepath.Join(o.fs.root, o.remote), in)
	if err != nil {
		return err
	}

	return nil
}

// MimeType of an Object if known, "" otherwise
func (o *Object) MimeType(ctx context.Context) string {
	return o.contentType
}

// From https://github.com/charmbracelet/charm/blob/9d0f28b6e656e8b22170a3ab12f5121d7c72b8ea/fs/fs.go#L184
//
// Takes a io.Reader instead of an fs.File, as we can't always stat stat
// remote objects.
func (f *Fs) writeFile(ctx context.Context, name string, in io.Reader) error {
	fs.Infof(f, "writing remote file %s", name)
	ebuf := bytes.NewBuffer(nil)
	eb, err := f.crypt.NewEncryptedWriter(ebuf)
	if err != nil {
		return err
	}

	if _, err = io.Copy(eb, in); err != nil {
		return err
	}

	if err := eb.Close(); err != nil {
		return err
	}
	eb.Close() //nolint:errcheck
	databuf := bytes.NewBuffer(nil)
	w := multipart.NewWriter(databuf)
	if _, err := w.CreateFormFile("data", name); err != nil {
		return err
	}
	headlen := databuf.Len()
	header := make([]byte, headlen)
	if _, err := databuf.Read(header); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	w.Close() //nolint:errcheck
	bounlen := databuf.Len()
	boun := make([]byte, bounlen)
	if _, err := databuf.Read(boun); err != nil {
		return err
	}
	// headlen is the length of the multipart part header, bounlen is the length of the multipart boundary footer.
	contentLength := int64(headlen) + int64(ebuf.Len()) + int64(bounlen)
	// pipe the multipart request to the server
	rr, rw := io.Pipe()
	defer rr.Close() // nolint:errcheck
	errs, _ := errgroup.WithContext(ctx)
	errs.Go(func() error {
		defer rw.Close() // nolint:errcheck

		// write multipart header
		if _, err := rw.Write(header); err != nil {
			return err
		}
		// chunk the read data into 64MB chunks
		buf := make([]byte, 1024*1024*64)
		for {
			n, err := ebuf.Read(buf)
			if err != nil {
				break
			}
			if _, err := rw.Write(buf[:n]); err != nil {
				return err
			}
		}
		// write multipart boundary
		_, err := rw.Write(boun)
		return err
	})

	ep, err := f.cfs.EncryptPath(name)
	if err != nil {
		return err
	}
	// FIXME: mode is hardcoded here, not sure if there's a way to get mode
	// from every src object
	path := fmt.Sprintf("/v1/fs/%s?mode=436", ep)
	headers := http.Header{
		"Content-Type":   {w.FormDataContentType()},
		"Content-Length": {fmt.Sprintf("%d", contentLength)},
	}
	resp, err := f.charmClient.AuthedRequest("POST", path, headers, rr)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return errs.Wait()
}
