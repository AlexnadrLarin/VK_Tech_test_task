package models

type Poll struct {
	ID        string
	Creator   string
	Question  string
	Voters    map[string]bool
	Options   map[string]int
	Closed    bool
}
