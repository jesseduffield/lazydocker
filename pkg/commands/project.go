package commands

type Project struct {
	Name string
}

func (self *Project) GetDisplayStrings(isFocused bool) []string {
	return []string{self.Name}
}
