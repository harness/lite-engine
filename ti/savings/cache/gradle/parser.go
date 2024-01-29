package gradle

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"

	"github.com/harness/ti-client/types"
	"github.com/mattn/go-zglob"
	"github.com/sirupsen/logrus"
	"golang.org/x/net/html"
)

func ParseSavings(workspace string, log *logrus.Logger, logErr bool) (types.IntelligenceExecutionState, int, error) {
	cacheState := types.FULL_RUN
	totalBuildTime := 0

	path := fmt.Sprintf("%s/build/reports/profile/*.html", workspace)
	files, err := zglob.Glob(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cacheState, totalBuildTime, fmt.Errorf("no profiles present")
		}
		if logErr {
			log.WithError(err).WithField("path", path).
				Errorln("errored while trying to resolve path regex")
		}
		return cacheState, totalBuildTime, err
	}
	if len(files) == 0 {
		return cacheState, totalBuildTime, fmt.Errorf("no profiles present")
	}

	foundProfile := false
	for _, file := range files {
		htmlContent, err := readHTMLFromFile(file)
		if err != nil {
			return cacheState, totalBuildTime, err
		}
		reader := strings.NewReader(htmlContent)
		doc, err := html.Parse(reader)
		if err != nil {
			return cacheState, totalBuildTime, err
		}
		buildTime, err := parseBuildTimeFromProfile(doc)
		if err == nil {
			foundProfile = true
			totalBuildTime += buildTime
			if strings.Contains(htmlContent, "FROM-CACHE") {
				cacheState = types.OPTIMIZED
			}
		}
	}
	if !foundProfile {
		return cacheState, totalBuildTime, errors.New("no valid gradle profile html found")
	}
	return cacheState, totalBuildTime, nil
}

func parseBuildTimeFromProfile(n *html.Node) (int, error) {
	bodyElement := getElementFromNode(n, "body")
	if bodyElement == nil {
		return 0, errors.New("no body present")
	}
	contentDiv := getDivByID(bodyElement, "content")
	if contentDiv == nil {
		return 0, errors.New("no content present")
	}
	tabs := getDivByID(contentDiv, "tabs")
	if tabs == nil {
		return 0, errors.New("no tabs present")
	}

	// Summary Div
	summaryDiv := getDivByID(tabs, "tab0")
	if summaryDiv == nil {
		return 0, errors.New("no summary present")
	}
	buildTimeStr := extractValueFromTable(summaryDiv, "Total Build Time")
	totalBuildTime := parseGradleVerseTimeMs(buildTimeStr)

	return totalBuildTime, nil
}

func extractValueFromTable(n *html.Node, description string) (value string) {
	if n == nil {
		return value
	}
	var traverse func(*html.Node)
	traverse = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "table" {
			for d := node.FirstChild; d != nil; d = d.NextSibling {
				if d.Type == html.ElementNode && d.Data == "tbody" {
					duration := getValueFromTBody(d, description)
					if duration != "" {
						value = duration
						return
					}
				}
			}
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			traverse(c)
		}
	}
	traverse(n)
	return value
}

func getValueFromTBody(n *html.Node, description string) string {
	if n == nil {
		return ""
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == "tr" {
			title, duration, valid := getTitleAndValueFromTR(c)
			if valid && title == description {
				return duration
			}
		}
	}
	return ""
}

func getTitleAndValueFromTR(n *html.Node) (string, string, bool) {
	if n == nil {
		return "", "", false
	}
	values := make([]string, 0)
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == "td" {
			textNode := getTextNodeFromTR(c)
			if textNode != nil && textNode.Data != "" {
				values = append(values, textNode.Data)
			}
		}
	}
	if len(values) == 2 {
		return values[0], values[1], true
	}
	return "", "", false
}

func getTextNodeFromTR(n *html.Node) *html.Node {
	if n == nil {
		return nil
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode {
			return c
		}
	}
	return nil
}

func readHTMLFromFile(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	content, err := ioutil.ReadAll(file)
	if err != nil {
		return "", err
	}

	return string(content), nil
}

func getElementFromNode(n *html.Node, elementData string) (element *html.Node) {
	if n == nil {
		return element
	}
	var traverse func(*html.Node)
	traverse = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == elementData {
			element = node
			return
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			traverse(c)
		}
	}
	traverse(n)
	return element
}

func getDivByID(n *html.Node, id string) (divNode *html.Node) {
	if n == nil {
		return divNode
	}
	var traverse func(*html.Node)
	traverse = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "div" {
			for _, attr := range node.Attr {
				if attr.Key == "id" && attr.Val == id {
					divNode = node
					return
				}
			}
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			traverse(c)
		}
	}
	traverse(n)
	return divNode
}

func parseGradleVerseTimeMs(t string) int {
	var dayStr, hourStr, minStr, secondStr string
	if strings.Contains(t, "d") {
		split := strings.Split(t, "d")
		dayStr = strings.TrimSpace(split[0])
		t = split[1]
	}
	if strings.Contains(t, "h") {
		split := strings.Split(t, "h")
		hourStr = strings.TrimSpace(split[0])
		t = split[1]
	}
	if strings.Contains(t, "m") {
		split := strings.Split(t, "m")
		minStr = strings.TrimSpace(split[0])
		t = split[1]
	}
	if strings.Contains(t, "s") {
		split := strings.Split(t, "s")
		secondStr = strings.TrimSpace(split[0])
	}
	durationMs := 0
	if days, err := strconv.Atoi(dayStr); err == nil {
		durationMs += days * 24 * 60 * 60 * 1000
	}
	if hours, err := strconv.Atoi(hourStr); err == nil {
		durationMs += hours * 60 * 60 * 1000
	}
	if minutes, err := strconv.Atoi(minStr); err == nil {
		durationMs += minutes * 60 * 1000
	}
	if seconds, err := strconv.ParseFloat(secondStr, 64); err == nil {
		durationMs += int(seconds * 1000)
	}
	return durationMs
}
