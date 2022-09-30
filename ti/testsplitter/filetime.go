package testsplitter

/*
	Map for <FileName, TimeDuration> for loading timing information.
	The time is a metric that's used to calculate weight of the split/bucket
	It doesn't necessarily have time units. For example, we could split the
	files based on filesize or lines of code in which case the time field
	indicates lines.
*/
type fileTimesListItem struct {
	name string
	time float64
}
type fileTimesList []fileTimesListItem

func (l fileTimesList) Len() int { return len(l) }

// Less Sorts by time descending, then by name ascending.
// Comparator in Golang is Less()
// Sort by name is required for deterministic order across machines
func (l fileTimesList) Less(i, j int) bool {
	return l[i].time > l[j].time ||
		(l[i].time == l[j].time && l[i].name < l[j].name)
}

func (l fileTimesList) Swap(i, j int) {
	temp := l[i]
	l[i] = l[j]
	l[j] = temp
}
