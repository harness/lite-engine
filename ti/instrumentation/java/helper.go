package java

import (
	"bufio"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/mattn/go-zglob"
	"github.com/sirupsen/logrus"

	"github.com/harness/lite-engine/internal/filesystem"
)

var (
	javaAgentArg = "-javaagent:/addon/bin/java-agent.jar=%s"
)

// get list of all file paths matching a provided regex
func getFiles(path string) ([]string, error) {
	fmt.Println("path: ", path)
	matches, err := zglob.Glob(path)
	if err != nil {
		return []string{}, err
	}
	return matches, err
}

// detect java packages by reading all the files and parsing their package names
func DetectPkgs(workspace string, log *logrus.Logger, fs filesystem.FileSystem) ([]string, error) { // nolint:gocyclo
	plist := []string{}
	excludeList := []string{"com.google"} // exclude any instances of these packages from the package list

	files, err := getFiles(fmt.Sprintf("%s/**/*.java", workspace))
	if err != nil {
		return plist, err
	}
	kotlinFiles, err := getFiles(fmt.Sprintf("%s/**/*.kt", workspace))
	if err != nil {
		return plist, err
	}
	// Create a list with all *.java and *.kt file paths
	files = append(files, kotlinFiles...)
	fmt.Println("files: ", files)
	m := make(map[string]struct{})
	for _, f := range files {
		absPath, err := filepath.Abs(f)
		if err != nil {
			log.WithError(err).WithField("file", f).Errorln("could not get absolute path")
			continue
		}
		// TODO: (Vistaar)
		// This doesn't handle some special cases right now such as when there is a package
		// present in a multiline comment with multiple opening and closing comments.
		// We will require to read all the lines together to handle this.
		err = fs.ReadFile(absPath, func(fr io.Reader) error {
			scanner := bufio.NewScanner(fr)
			commentOpen := false
			for scanner.Scan() {
				l := strings.TrimSpace(scanner.Text())
				if strings.Contains(l, "/*") {
					commentOpen = true
				}
				if strings.Contains(l, "*/") {
					commentOpen = false
					continue
				}
				if commentOpen || strings.HasPrefix(l, "//") {
					continue
				}
				prev := ""
				pkg := ""
				for _, token := range strings.Fields(l) {
					if prev == "package" {
						pkg = token
						break
					}
					prev = token
				}
				if pkg != "" {
					pkg = strings.TrimSuffix(pkg, ";")
					tokens := strings.Split(pkg, ".")
					prefix := false
					for _, exclude := range excludeList {
						if strings.HasPrefix(pkg, exclude) {
							logrus.Infoln(fmt.Sprintf("Found package: %s having same package prefix as: %s. Excluding this package from the list...", pkg, exclude))
							prefix = true
							break
						}
					}
					if !prefix {
						pkg = tokens[0]
						if len(tokens) > 1 {
							pkg = pkg + "." + tokens[1]
						}
					}
					if pkg == "" {
						continue
					}
					if _, ok := m[pkg]; !ok {
						plist = append(plist, pkg)
						m[pkg] = struct{}{}
					}
					return nil
				}
			}
			if err = scanner.Err(); err != nil {
				logrus.WithError(err).Errorln("could not scan all the files")
				return err
			}
			return nil
		})
		if err != nil {
			logrus.WithError(err).Errorln("had issues while trying to auto detect java packages")
		}
	}
	return plist, nil
}
