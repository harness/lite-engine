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

var _callgraph_avsc = []byte("\x1f\x8b\x08\x00\x00\x00\x00\x00\x00\xff\xe4\x53\xbd\x6e\xfb\x20\x10\xdf\xf3\x14\xe8\xe6\x28\x0f\xe0\xf5\x3f\xfd\xb7\x2a\x6b\x95\xe1\x6a\x2e\xf6\xa9\x18\x2c\x8e\x56\x8a\xac\xbc\x7b\x85\x1d\x52\x63\x7b\x88\xa8\xda\xa1\x65\x82\xe3\xf7\x71\x1f\x30\xec\x94\x52\x0a\x2c\x76\x04\x95\x82\x27\xf4\x64\x03\xec\xa7\x68\xb8\xf4\x04\x15\x78\xaa\x9d\xd7\x29\x18\xa1\xd2\x63\x4d\xa0\x2a\x05\x81\x0f\x2d\x7a\x4b\x22\x07\x76\x09\x72\x66\x32\x5a\xa0\x7a\x1e\x8f\x71\x0d\xf7\xdd\xcc\x0e\xac\xd3\x24\x37\xd2\xfd\x6e\x32\xcd\x09\xb3\x0b\x05\xe8\x3d\x5e\x60\xaf\xd4\x1a\xc2\x81\x3a\xd9\x22\xcf\x5d\xff\xb5\x6c\xf4\xc2\x75\x61\x92\x97\xbc\x02\xad\xeb\x5b\xae\x61\x32\x8b\x1d\xea\x28\xb4\x4e\xc3\xfe\xa6\x1d\x43\x12\x3c\xdb\x06\xae\xdb\xf2\x0b\x7e\x8f\xf5\x2b\x36\x54\x2e\xc0\xb9\x39\xdb\xf0\x20\xb1\x36\x28\xf2\xbf\x94\xdd\xa3\xc7\x4e\xca\xd3\x1e\xdd\xcb\xe9\x23\xab\x88\x1d\xbd\xd1\x18\x39\xd2\xd9\x50\x1d\xd8\xd9\x4c\xe7\xc5\x39\x43\x68\x1f\x13\x3a\xb3\xd9\xce\x62\x93\x7b\x5a\x45\x73\xdc\xe7\x69\x66\xbe\xfd\xb5\x02\x49\x38\x92\xc1\x98\xfe\xaf\xff\x62\xe2\xde\x7c\x4d\x85\xef\x34\x76\x2a\x7b\x67\x43\xca\x30\x75\x21\xd5\x3c\x89\x7e\xfb\xe8\xde\x59\x1a\x8f\x7d\xfb\xe5\xf1\xfd\x81\xe1\x69\x92\xc0\x36\xb5\xe9\xe7\x66\x38\xee\x4e\x6a\x77\xdd\x7d\x04\x00\x00\xff\xff\xb2\x83\xa4\xfd\x41\x07\x00\x00")

func callgraph_avsc() ([]byte, error) {
	return bindata_read(
		_callgraph_avsc,
		"callgraph.avsc",
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
	"callgraph.avsc": callgraph_avsc,
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
	"callgraph.avsc": {callgraph_avsc, map[string]*_bintree_t{}},
}}
