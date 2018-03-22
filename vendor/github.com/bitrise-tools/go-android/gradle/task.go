package gradle

import (
	"fmt"
	"strings"
)

// Task ...
type Task struct {
	name   string
	module Module
}

// GetVariants ...
func (task *Task) GetVariants() (Variants, error) {
	tasksOutput, err := getGradleOutput(task.module.project.location, task.module.name+"tasks")
	if err != nil {
		return nil, fmt.Errorf("%s, %s", tasksOutput, err)
	}
	return task.parseVariants(tasksOutput), nil
}

func (task *Task) parseVariants(gradleOutput string) Variants {
	//example gradleOutput:
	//"
	// lintMyflavorokStaging - Runs lint on the MyflavorokStaging build.
	// lintMyflavorRelease - Runs lint on the MyflavorRelease build.
	// lintVitalMyflavorRelease - Runs lint on the MyflavorRelease build.
	// lintMyflavorStaging - Runs lint on the MyflavorStaging build."
	var tasks []string
lines:
	for _, l := range strings.Split(gradleOutput, "\n") {
		// l: " lintMyflavorokStaging - Runs lint on the MyflavorokStaging build."
		l = strings.TrimSpace(l)
		// l: "lintMyflavorokStaging - Runs lint on the MyflavorokStaging build."
		if l == "" {
			continue
		}
		l = strings.Split(l, " ")[0]
		// l: "lintMyflavorokStaging"
		if strings.HasPrefix(l, task.name) {
			// task.name: "lint"
			// strings.HasPrefix will match lint and lintVital prefix also, we won't need lintVital so it is a conflict
			for _, conflict := range conflicts[task.name] {
				if strings.HasPrefix(l, conflict) {
					// if line has conflicting prefix don't do further checks with this line, skip...
					continue lines
				}
			}
			l = strings.TrimPrefix(l, task.name)
			// l: "MyflavorokStaging"
			if l == "" {
				continue
			}
			tasks = append(tasks, l)
		}
	}
	return cleanStringSlice(tasks)
}

// Run ...
func (task *Task) Run(variants Variants) error {
	var args []string
	for _, variant := range variants {
		args = append(args, task.module.name+task.name+variant)
	}
	return runGradleCommand(task.module.project.location, args...)
}
