package step

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bitrise-io/go-android/gradle"
	"github.com/bitrise-io/go-steputils/stepconf"
	"github.com/bitrise-io/go-steputils/tools"
	"github.com/bitrise-io/go-utils/command"
	"github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-io/go-utils/pathutil"
	"github.com/kballard/go-shellquote"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// Input ...
type Input struct {
	ProjectLocation string `env:"project_location,dir"`
	AppPathPattern  string `env:"app_path_pattern,required"`
	Variant         string `env:"variant"`
	Module          string `env:"module"`
	BuildType       string `env:"build_type,opt[apk,aab]"`
	Arguments       string `env:"arguments"`
	CacheLevel      string `env:"cache_level"` // Deprecated
	DeployDir       string `env:"BITRISE_DEPLOY_DIR,dir"`
}

// Config ...
type Config struct {
	ProjectLocation string

	Variants []string
	Module   string

	AppPathPattern string
	AppType        string
	Arguments      []string

	DeployDir string
}

// Result ...
type Result struct {
	appFiles     []gradle.Artifact
	appType      string
	mappingFiles []gradle.Artifact
}

// AndroidBuild ...
type AndroidBuild struct {
	inputParser stepconf.InputParser
	logger      log.Logger
	cmdFactory  command.Factory
}

// GradleProjectWrapper ...
type GradleProjectWrapper interface {
	FindArtifacts(generatedAfter time.Time, pattern string, includeModuleInName bool) ([]gradle.Artifact, error)
}

const (
	apkAppType = "apk"
	aabAppType = "aab"

	apkEnvKey     = "BITRISE_APK_PATH"
	apkListEnvKey = "BITRISE_APK_PATH_LIST"

	aabEnvKey     = "BITRISE_AAB_PATH"
	aabListEnvKey = "BITRISE_AAB_PATH_LIST"

	mappingFileEnvKey  = "BITRISE_MAPPING_PATH"
	mappingFilePattern = "*build/*/mapping.txt"
)

// NewAndroidBuild ...
func NewAndroidBuild(inputParser stepconf.InputParser, logger log.Logger, cmdFactory command.Factory) *AndroidBuild {
	return &AndroidBuild{inputParser: inputParser, logger: logger, cmdFactory: cmdFactory}
}

// ProcessConfig ...
func (a AndroidBuild) ProcessConfig() (Config, error) {
	var input Input
	err := a.inputParser.Parse(&input)
	if err != nil {
		return Config{}, err
	}
	stepconf.Print(input)

	args, err := shellquote.Split(input.Arguments)
	if err != nil {
		return Config{}, fmt.Errorf("failed to parse arguments: %s", err)
	}

	if input.CacheLevel != "" {
		a.logger.Warnf("The cache_level Input (branch-based legacy caching) is deprecated, please use dedicated Key-based caching Steps instead.")
	}

	return Config{
		ProjectLocation: input.ProjectLocation,
		AppPathPattern:  input.AppPathPattern,
		Variants:        parseVariants(input.Variant),
		Module:          input.Module,
		AppType:         input.BuildType,
		Arguments:       args,
		DeployDir:       input.DeployDir,
	}, nil
}

// Run ...
func (a AndroidBuild) Run(cfg Config) (Result, error) {
	gradleProject, err := gradle.NewProject(cfg.ProjectLocation, a.cmdFactory)
	if err != nil {
		return Result{}, fmt.Errorf("failed to open Gradle project: %s", err)
	}

	started := time.Now()

	if err := a.executeGradleBuild(cfg); err != nil {
		return Result{}, err
	}

	a.logger.Println()
	a.logger.Infof("Export Artifacts:")

	var appPathPatterns = strings.Split(cfg.AppPathPattern, "\n")
	appArtifacts, err := a.getArtifacts(gradleProject, started, appPathPatterns, false)
	if err != nil {
		return Result{}, fmt.Errorf("failed to find app artifacts: %v", err)
	}

	mappings, err := a.getArtifacts(gradleProject, started, []string{mappingFilePattern}, true)
	if err != nil {
		return Result{}, fmt.Errorf("failed to find mapping files, error: %v", err)
	}

	a.printAppSearchInfo(appArtifacts, appPathPatterns)

	a.logger.Donef("Exporting artifacts with the selected app type: %s", cfg.AppType)
	// Filter appFiles by build type
	var filteredArtifacts []gradle.Artifact
	for _, a := range appArtifacts {
		if filepath.Ext(a.Path) == fmt.Sprintf(".%s", cfg.AppType) {
			filteredArtifacts = append(filteredArtifacts, a)
		}
	}

	if len(filteredArtifacts) == 0 {
		a.logger.Warnf("No app artifacts found with patterns:\n%s", cfg.AppPathPattern)
		a.logger.Warnf("If you have changed default APK, AAB export path in your gradle files then you might need to change app_path_pattern accordingly.")
	}

	return Result{
		appFiles:     filteredArtifacts,
		appType:      cfg.AppType,
		mappingFiles: mappings,
	}, nil
}

// Export ...
func (a AndroidBuild) Export(result Result, deployDir string) error {
	exportedArtifactPaths, err := a.exportArtifacts(result.appFiles, deployDir)
	if err != nil {
		return fmt.Errorf("failed to export artifact: %v", err)
	}

	if len(exportedArtifactPaths) == 0 {
		return fmt.Errorf("could not export any app artifacts")
	}

	lastExportedArtifact := exportedArtifactPaths[len(exportedArtifactPaths)-1]

	// Use the correct env key for the selected build type
	var envKey string
	if result.appType == apkAppType {
		envKey = apkEnvKey
	} else {
		envKey = aabEnvKey
	}
	if err := tools.ExportEnvironmentWithEnvman(envKey, lastExportedArtifact); err != nil {
		return fmt.Errorf("failed to export environment variable: %s", envKey)
	}
	a.logger.Println()
	a.logger.Printf("  Env    [ $%s = $BITRISE_DEPLOY_DIR/%s ]", envKey, filepath.Base(lastExportedArtifact))

	var paths, sep string
	for _, path := range exportedArtifactPaths {
		paths += sep + "$BITRISE_DEPLOY_DIR/" + filepath.Base(path)
		sep = "| \\\n" + strings.Repeat(" ", 11)
	}

	// Use the correct env key for the selected build type
	if result.appType == apkAppType {
		envKey = apkListEnvKey
	} else {
		envKey = aabListEnvKey
	}
	if err := tools.ExportEnvironmentWithEnvman(envKey, strings.Join(exportedArtifactPaths, "|")); err != nil {
		return fmt.Errorf("failed to export environment variable: %s", envKey)
	}
	a.logger.Printf("  Env    [ $%s = %s ]", envKey, paths)

	a.logger.Println()

	a.logger.Infof("Export mapping files:")
	a.logger.Println()

	if len(result.mappingFiles) == 0 {
		a.logger.Printf("No mapping files found with pattern: %s", mappingFilePattern)
		a.logger.Printf("You might have changed default mapping file export path in your gradle files or obfuscation is not enabled in your project.")
		return nil
	}

	exportedArtifactPaths, err = a.exportArtifacts(result.mappingFiles, deployDir)
	if err != nil {
		return fmt.Errorf("failed to export artifact: %v", err)
	}

	if len(exportedArtifactPaths) == 0 {
		return fmt.Errorf("could not export any mapping.txt")
	}

	lastExportedArtifact = exportedArtifactPaths[len(exportedArtifactPaths)-1]

	a.logger.Println()
	if err := tools.ExportEnvironmentWithEnvman(mappingFileEnvKey, lastExportedArtifact); err != nil {
		return fmt.Errorf("failed to export environment variable: %s", mappingFileEnvKey)
	}
	a.logger.Printf("  Env    [ $%s = $BITRISE_DEPLOY_DIR/%s ]", mappingFileEnvKey, filepath.Base(lastExportedArtifact))

	return nil
}

func gradleTaskName(appType, module, variant string) (string, error) {
	var task string

	// Note: the task should not start with a colon because that syntax only works from the
	// root folder, but the step has a project path input and we "cd" into that dir. It's a valid step configuration
	// to define a submodule's path as project path and in this case `:assembleDebug` doesn't work, only `assembleDebug`
	// This is only relevant when the module is NOT defined, a module should always have the colon prefix.
	if appType == apkAppType {
		task = "assemble"
	} else if appType == aabAppType {
		task = "bundle"
	} else {
		return "", fmt.Errorf("invalid app type: %s", appType)
	}

	// If variant is not defined, Gradle will execute the task for all variants (eg. assemble -> assembleDebug, assembleRelease)
	if variant != "" {
		task = task + cases.Title(language.English, cases.NoLower).String(variant)
	}

	// If module is not defined, Gradle will execute the task on all modules in the project
	if module != "" {
		rawModule := strings.TrimPrefix(module, ":")
		task = fmt.Sprintf(":%s:%s", rawModule, task)
	}

	return task, nil
}

func (a AndroidBuild) getArtifacts(gradleProject GradleProjectWrapper, started time.Time, patterns []string, includeModule bool) (artifacts []gradle.Artifact, err error) {
	for _, pattern := range patterns {
		afs, err := gradleProject.FindArtifacts(started, pattern, includeModule)
		if err != nil {
			a.logger.Warnf("Failed to find artifact with pattern ( %s ), error: %s", pattern, err)
			continue
		}
		artifacts = append(artifacts, afs...)
	}

	if len(artifacts) == 0 {
		if !started.IsZero() {
			a.logger.Warnf("No app files found with patterns: %s that has modification time after: %s", strings.Join(patterns, ", "), started)
			a.logger.Warnf("Retrying without modtime check....")
			a.logger.Println()
			return a.getArtifacts(gradleProject, time.Time{}, patterns, includeModule)
		}
		a.logger.Warnf("No app files found with pattern: %s without modtime check", strings.Join(patterns, ", "))
	}
	return
}

func (a AndroidBuild) executeGradleBuild(cfg Config) error {
	a.logger.Infof("Run build:")

	var tasks []string
	for _, variant := range cfg.Variants {
		taskName, err := gradleTaskName(cfg.AppType, cfg.Module, variant)
		if err != nil {
			return err
		}
		tasks = append(tasks, taskName)
	}

	cmdArgs := append(tasks, cfg.Arguments...)
	cmdOpts := command.Opts{
		Dir:    cfg.ProjectLocation,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
	absPath, err := filepath.Abs(cfg.ProjectLocation)
	if err != nil {
		return err
	}
	cmd := a.cmdFactory.Create(filepath.Join(absPath, "gradlew"), cmdArgs, &cmdOpts)

	a.logger.Println()
	a.logger.Donef("$ " + cmd.PrintableCommandArgs())
	a.logger.Println()

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("build task failed: %v", err)
	}

	return nil
}

func (a AndroidBuild) printAppSearchInfo(appArtifacts []gradle.Artifact, appPathPatterns []string) {
	var artPaths []string
	for _, a := range appArtifacts {
		artPaths = append(artPaths, a.Name)
	}

	a.logger.Donef("Used patterns for generated artifact search:")
	a.logger.Printf(strings.Join(appPathPatterns, "\n"))
	a.logger.Println()
	a.logger.Donef("Found app artifacts:")
	a.logger.Printf(strings.Join(artPaths, "\n"))
	a.logger.Println()
}

func (a AndroidBuild) exportArtifacts(artifacts []gradle.Artifact, deployDir string) ([]string, error) {
	var paths []string
	for _, artifact := range artifacts {
		exists, err := pathutil.IsPathExists(filepath.Join(deployDir, artifact.Name))
		if err != nil {
			return nil, fmt.Errorf("failed to check path, error: %v", err)
		}

		artifactName := filepath.Base(artifact.Path)

		if exists {
			timestamp := time.Now().Format("20060102150405")
			ext := filepath.Ext(artifact.Name)
			name := strings.TrimSuffix(filepath.Base(artifact.Name), ext)
			artifact.Name = fmt.Sprintf("%s-%s%s", name, timestamp, ext)
		}

		a.logger.Printf("  Export [ %s => $BITRISE_DEPLOY_DIR/%s ]", artifactName, artifact.Name)

		if err := artifact.Export(deployDir); err != nil {
			a.logger.Warnf("failed to export artifact (%s), error: %v", artifact.Path, err)
			continue
		}

		paths = append(paths, filepath.Join(deployDir, artifact.Name))
	}
	return paths, nil
}

// parseVariants returns the list of variants from the raw step input string.
// The variants are primarily split by linebreaks, but the step used to split by the "\n" substring in the past,
// so we also handle that for backwards compatibility.
func parseVariants(input string) []string {
	var variants []string

	for _, line := range strings.Split(input, "\n") {
		variantsPerLine := strings.Split(line, `\n`)
		variants = append(variants, variantsPerLine...)
	}

	return variants
}
