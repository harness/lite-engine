package schema

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"strings"
)

func bindata_read(data []byte, name string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewBuffer(data))
	if err != nil {
		return nil, fmt.Errorf("Read %q: %v", name, err)
	}

	var buf bytes.Buffer
	_, err = io.Copy(&buf, gz)
	gz.Close()

	if err != nil {
		return nil, fmt.Errorf("Read %q: %v", name, err)
	}

	return buf.Bytes(), nil
}

var _visgraph_avsc = []byte("\x1f\x8b\x08\x00\x00\x00\x00\x00\x00\xff\xec\x91\x41\x6a\xc4\x30\x0c\x45\xf7\x39\x85\xd0\x3a\xcc\x01\xb2\xed\x05\xba\x2f\x5d\x88\x44\x9d\x88\xb1\x9d\x20\xb9\x85\x10\xe6\xee\xc5\xf6\xc4\xcd\x94\x69\x21\xdb\xd2\x2c\x62\xf3\xf5\xfe\x97\x8c\xd6\x06\x00\x03\x79\xc6\x0e\xf0\x99\x94\x43\xc4\x36\x69\x71\x99\x19\x3b\x54\xee\x27\x1d\x8a\x94\x30\x9b\xa9\x67\x84\x0e\x30\xca\x69\x24\x0d\x6c\x76\x92\xa9\x00\x6f\xc2\x6e\x30\xec\x5e\x1a\x00\x80\x35\xff\x6b\x3c\x5e\x78\xb1\xcc\x65\xb1\xe4\x6f\x4c\x15\x00\x49\x95\x96\xca\x01\xa0\x44\xf6\xb6\x47\xbf\x22\x9f\x46\x71\xc3\x8e\x7d\x38\x77\x2d\xdd\x8f\xb7\x7d\x6b\x09\x4b\x4f\xf2\x1c\xc7\x69\xc0\xf6\x96\x92\x24\x8b\x2a\xe1\x8c\xd7\xf6\x27\xcf\x4c\xfd\x85\xce\x7c\xcc\x24\xf7\x4d\x24\xc4\x5f\x3b\x28\x79\x3b\xd6\xa0\x77\x64\x07\x2d\x99\x7c\xe4\xd8\x19\x5e\xeb\x7d\x53\xcb\x79\x4b\xfd\xbe\xf1\x0f\x72\xef\xfc\xbf\xf3\x3f\xba\xf3\x26\xd5\xae\xcd\x67\x00\x00\x00\xff\xff\x09\x88\x36\x97\x42\x04\x00\x00")

func visgraph_avsc() ([]byte, error) {
	return bindata_read(
		_visgraph_avsc,
		"visgraph.avsc",
	)
}

// Asset loads and returns the asset for the given name.
// It returns an error if the asset could not be found or
// could not be loaded.
func Asset(name string) ([]byte, error) {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	if f, ok := _bindata[cannonicalName]; ok {
		return f()
	}
	return nil, fmt.Errorf("Asset %s not found", name)
}

// AssetNames returns the names of the assets.
func AssetNames() []string {
	names := make([]string, 0, len(_bindata))
	for name := range _bindata {
		names = append(names, name)
	}
	return names
}

// _bindata is a table, holding each asset generator, mapped to its name.
var _bindata = map[string]func() ([]byte, error){
	"visgraph.avsc": visgraph_avsc,
}

// AssetDir returns the file names below a certain
// directory embedded in the file by go-bindata.
// For example if you run go-bindata on data/... and data contains the
// following hierarchy:
//     data/
//       foo.txt
//       img/
//         a.png
//         b.png
// then AssetDir("data") would return []string{"foo.txt", "img"}
// AssetDir("data/img") would return []string{"a.png", "b.png"}
// AssetDir("foo.txt") and AssetDir("notexist") would return an error
// AssetDir("") will return []string{"data"}.
func AssetDir(name string) ([]string, error) {
	node := _bintree
	if len(name) != 0 {
		cannonicalName := strings.Replace(name, "\\", "/", -1)
		pathList := strings.Split(cannonicalName, "/")
		for _, p := range pathList {
			node = node.Children[p]
			if node == nil {
				return nil, fmt.Errorf("Asset %s not found", name)
			}
		}
	}
	if node.Func != nil {
		return nil, fmt.Errorf("Asset %s not found", name)
	}
	rv := make([]string, 0, len(node.Children))
	for name := range node.Children {
		rv = append(rv, name)
	}
	return rv, nil
}

type _bintree_t struct {
	Func     func() ([]byte, error)
	Children map[string]*_bintree_t
}

var _bintree = &_bintree_t{nil, map[string]*_bintree_t{
	"visgraph.avsc": {visgraph_avsc, map[string]*_bintree_t{}},
}}
