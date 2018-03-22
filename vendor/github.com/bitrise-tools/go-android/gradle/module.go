package gradle

// Module ...
type Module struct {
	project Project
	name    string
	tasks   []Task
}

// GetTask ...
func (module Module) GetTask(name string) *Task {
	return &Task{
		module: module,
		name:   name,
	}
}
