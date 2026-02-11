package gradle

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/harness/ti-client/types"
	gradleTypes "github.com/harness/ti-client/types/cache/gradle"
	"golang.org/x/net/html"
)

func parseGradleVerseTimeMs(t string) int64 {
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
		durationMs += days * 24 * 60 * 60 * 1000 //nolint:mnd
	}
	if hours, err := strconv.Atoi(hourStr); err == nil {
		durationMs += hours * 60 * 60 * 1000 //nolint:mnd
	}
	if minutes, err := strconv.Atoi(minStr); err == nil {
		durationMs += minutes * 60 * 1000 //nolint:mnd
	}
	if seconds, err := strconv.ParseFloat(secondStr, 64); err == nil {
		durationMs += int(seconds * 1000) //nolint:mnd
	}
	return int64(durationMs)
}

func parseProfileFromHtml(n *html.Node) (gradleTypes.Profile, bool, error) { //nolint:gocyclo,revive,staticcheck
	profile := gradleTypes.Profile{}
	rootNode := JsonNode{}
	rootNode.populateFrom(n)
	if len(rootNode.Elements) != 1 {
		return profile, false, fmt.Errorf("invalid profile html")
	} else if rootNode.Elements[0].Name != "html" {
		return profile, false, fmt.Errorf("profile does not have html element")
	}

	htmlNode := rootNode.Elements[0]
	if len(htmlNode.Elements) != 2 || htmlNode.Elements[1].Name != "body" {
		return profile, false, fmt.Errorf("profile html does not have expected number of elements")
	}

	_ = htmlNode.Elements[0]     // head
	body := htmlNode.Elements[1] // body
	if len(body.Elements) != 1 || body.Elements[0].Name != "div" || body.Elements[0].Id != "content" {
		return profile, false, fmt.Errorf("html body does not have valid div")
	}
	contentDiv := body.Elements[0]
	if len(contentDiv.Elements) != 4 { //nolint:mnd
		return profile, false, fmt.Errorf("invalid content div")
	}

	// Parse Cmd
	cmd, err := parseCmdFromContentDiv(&contentDiv)
	if err == nil {
		profile.Cmd = cmd
	}

	// Parse Build Time
	buildTimeMs, taskExectionTimeMs, err := parseBuildTimeFromContentDiv(&contentDiv)
	if err == nil {
		if buildTimeMs != -1 {
			profile.BuildTimeMs = buildTimeMs
		}
		if taskExectionTimeMs != -1 {
			profile.TaskExecutionTimeMs = taskExectionTimeMs
		}
	}

	projects, err := parseProjectsFromContentDiv(&contentDiv)
	if err == nil {
		profile.Projects = projects
	}

	cached := false
	for _, g := range projects {
		for _, t := range g.Tasks {
			if t.State == "FROM-CACHE" {
				cached = true
			}
		}
	}
	return profile, cached, nil
}

func parseCmdFromContentDiv(contentDiv *JsonNode) (string, error) {
	if contentDiv == nil {
		return "", fmt.Errorf("empty content div")
	}
	header := contentDiv.Elements[1]
	if len(header.Elements) < 1 {
		return "", fmt.Errorf("invalid header for command")
	}
	cmd := header.Elements[0].Text
	cmd = strings.TrimSpace(cmd)
	if strings.HasPrefix(cmd, "Profiled build:") {
		cmd = strings.TrimPrefix(cmd, "Profiled build:")
		cmd = strings.TrimSpace(cmd)
		return cmd, nil
	}
	return "", fmt.Errorf("no command found in profile html")
}

func parseBuildTimeFromContentDiv(contentDiv *JsonNode) (int64, int64, error) { //nolint:gocritic
	if contentDiv == nil {
		return -1, -1, fmt.Errorf("empty content div")
	}
	tabs := contentDiv.Elements[2]
	if len(tabs.Elements) < 5 || tabs.Elements[1].Id != "tab0" {
		return -1, -1, fmt.Errorf("tabs element does not have tab0")
	}

	tab0 := tabs.Elements[1]
	if len(tab0.Elements) < 2 || tab0.Elements[1].Name != "table" {
		return -1, -1, fmt.Errorf("tab0 does not have a table")
	}

	table := tab0.Elements[1]
	if len(table.Elements) < 2 { //nolint:mnd
		return -1, -1, fmt.Errorf("table does not have a list of headings")
	}

	tableBody := table.Elements[1]
	buildTimeMs := int64(-1)
	taskExecutionTimeMs := int64(-1)
	for _, n := range tableBody.Elements {
		if len(n.Elements) != 2 { //nolint:mnd
			continue
		}
		title := n.Elements[0].Text
		value := n.Elements[1].Text

		if title == "Total Build Time" {
			buildTimeMs = parseGradleVerseTimeMs(value)
		}
		if title == "Task Execution" {
			taskExecutionTimeMs = parseGradleVerseTimeMs(value)
		}
	}
	return buildTimeMs, taskExecutionTimeMs, nil
}

func parseProjectsFromContentDiv(contentDiv *JsonNode) ([]gradleTypes.Project, error) {
	goals := make([]gradleTypes.Project, 0)
	if contentDiv == nil {
		return goals, fmt.Errorf("empty content div")
	}

	tabs := contentDiv.Elements[2]
	if len(tabs.Elements) < 5 || tabs.Elements[5].Id != "tab4" {
		return goals, fmt.Errorf("tabs element does not have tab4")
	}

	tab4 := tabs.Elements[5]
	if len(tab4.Elements) < 2 || tab4.Elements[1].Name != "table" {
		return goals, fmt.Errorf("tab4 does not have a table")
	}

	taskTable := tab4.Elements[1]
	if len(taskTable.Elements) < 2 { //nolint:mnd
		return goals, fmt.Errorf("task table does not have a list of tasks")
	}

	var goal gradleTypes.Project
	tBody := taskTable.Elements[1]
	for _, taskObj := range tBody.Elements {
		if len(taskObj.Elements) != 3 { //nolint:mnd
			continue
		}
		name := taskObj.Elements[0].Text
		duration := taskObj.Elements[1].Text
		state := taskObj.Elements[2].Text

		if state == "(total)" {
			// start of a new task
			goals = append(goals, goal)
			goal = gradleTypes.Project{
				Name:   name,
				TimeMs: parseGradleVerseTimeMs(duration),
				Tasks:  make([]gradleTypes.Task, 0),
			}
			continue
		}
		task := gradleTypes.Task{
			Name:   name,
			TimeMs: parseGradleVerseTimeMs(duration),
			State:  state,
		}
		goal.Tasks = append(goal.Tasks, task)
	}
	goals = append(goals, goal)
	return goals[1:], nil
}

// JsonNode is a JSON-ready representation of an HTML node.
type JsonNode struct { //nolint:revive,staticcheck
	// Name is the name/tag of the element
	Name string `json:"name,omitempty"`
	// Attributes contains the attributes of the element other than id, class, and href
	Attributes map[string]string `json:"attributes,omitempty"`
	// Class contains the class attribute of the element
	Class string `json:"class,omitempty"`
	// Id contains the id attribute of the element
	Id string `json:"id,omitempty"` //nolint:revive,staticcheck
	// Href contains the href attribute of the element
	Href string `json:"href,omitempty"`
	// Text contains the inner text of the element
	Text string `json:"text,omitempty"`
	// Elements contains the child elements of the element
	Elements []JsonNode `json:"elements,omitempty"`
}

func (n *JsonNode) populateFrom(htmlNode *html.Node) { //nolint:gocyclo
	if htmlNode == nil {
		return
	}
	switch htmlNode.Type { //nolint:exhaustive
	case html.ElementNode:
		n.Name = htmlNode.Data
	case html.DocumentNode:
		break
	default:
		return
	}

	var textBuffer bytes.Buffer
	if len(htmlNode.Attr) > 0 {
		n.Attributes = make(map[string]string)
		var a html.Attribute
		for _, a = range htmlNode.Attr {
			switch a.Key {
			case "class":
				n.Class = a.Val

			case "id":
				n.Id = a.Val

			case "href":
				n.Href = a.Val

			default:
				n.Attributes[a.Key] = a.Val
			}
		}
	}

	e := htmlNode.FirstChild
	for e != nil {
		switch e.Type { //nolint:exhaustive
		case html.TextNode:
			trimmed := strings.TrimSpace(e.Data)
			if len(trimmed) > 0 { //nolint:gocritic // emptyStringTest: intentional length check
				// mimic HTML text normalizing
				if textBuffer.Len() > 0 {
					textBuffer.WriteString(" ")
				}
				textBuffer.WriteString(trimmed)
			}
		case html.ElementNode:
			if n.Elements == nil {
				n.Elements = make([]JsonNode, 0)
			}
			var jsonElemNode JsonNode
			jsonElemNode.populateFrom(e)
			n.Elements = append(n.Elements, jsonElemNode)
		}

		e = e.NextSibling
	}
	if textBuffer.Len() > 0 {
		n.Text = textBuffer.String()
	}
}

func GetMetadataFromGradleMetrics(metrics *types.SavingsRequest) (totalTasks, cachedTasks int) {
	totalTasks = 0
	cachedTasks = 0

	for _, profile := range metrics.GradleMetrics.Profiles {
		for _, project := range profile.Projects {
			for _, task := range project.Tasks {
				totalTasks++
				if task.State == "FROM-CACHE" {
					cachedTasks++
				}
			}
		}
	}

	return totalTasks, cachedTasks
}
