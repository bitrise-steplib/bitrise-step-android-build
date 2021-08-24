package step

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	androidcache "github.com/bitrise-io/go-android/cache"
	"github.com/bitrise-io/go-android/gradle"
	"github.com/bitrise-io/go-steputils/cache"
	"github.com/bitrise-io/go-steputils/stepconf"
	"github.com/bitrise-io/go-steputils/tools"
	"github.com/bitrise-io/go-utils/command"
	"github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-io/go-utils/pathutil"
	"github.com/bitrise-io/go-utils/sliceutil"
	"github.com/kballard/go-shellquote"
)

// Input ...
type Input struct {
	ProjectLocation string `env:"project_location,dir"`
	AppPathPattern  string `env:"app_path_pattern,required"`
	Variant         string `env:"variant"`
	Module          string `env:"module"`
	BuildType       string `env:"build_type,opt[apk,aab]"`
	Arguments       string `env:"arguments"`
	CacheLevel      string `env:"cache_level,opt[none,only_deps,all]"`
	DeployDir       string `env:"BITRISE_DEPLOY_DIR,dir"`
}

// Config ...
type Config struct {
	ProjectLocation string

	Variant string
	Module  string

	AppPathPattern string
	AppType        appType
	Arguments      string

	CacheLevel cache.Level
	DeployDir  string
}

// Result ...
type Result struct {
	appFiles     []gradle.Artifact
	appType      appType
	mappingFiles []gradle.Artifact
}

// AndroidBuild ...
type AndroidBuild struct {
	inputParser stepconf.EnvParser
	logger      log.Logger
	cmdFactory  command.Factory
}

// GradleProjectWrapper ...
type GradleProjectWrapper interface {
	FindArtifacts(generatedAfter time.Time, pattern string, includeModuleInName bool) ([]gradle.Artifact, error)
}

type appType string

const (
	appTypeAPK = appType("apk")
	appTypeAAB = appType("aab")
)

const (
	apkEnvKey     = "BITRISE_APK_PATH"
	apkListEnvKey = "BITRISE_APK_PATH_LIST"

	aabEnvKey     = "BITRISE_AAB_PATH"
	aabListEnvKey = "BITRISE_AAB_PATH_LIST"

	mappingFileEnvKey  = "BITRISE_MAPPING_PATH"
	mappingFilePattern = "*build/*/mapping.txt"
)

var ignoredSuffixes = [...]string{"Classes", "Resources", "UnitTestClasses", "AndroidTestClasses", "AndroidTestResources"}

// NewAndroidBuild ...
func NewAndroidBuild(inputParser stepconf.EnvParser, logger log.Logger, cmdFactory command.Factory) *AndroidBuild {
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
	return Config{
		ProjectLocation: input.ProjectLocation,
		AppPathPattern:  input.AppPathPattern,
		Variant:         input.Variant,
		Module:          input.Module,
		AppType:         appType(input.BuildType),
		Arguments:       input.Arguments,
		CacheLevel:      cache.Level(input.CacheLevel),
		DeployDir:       input.DeployDir,
	}, nil
}

// Run ...
func (a AndroidBuild) Run(cfg Config) (Result, error) {
	gradleProject, err := gradle.NewProject(cfg.ProjectLocation, a.cmdFactory)
	if err != nil {
		return Result{}, fmt.Errorf("failed to open Gradle project: %s", err)
	}

	var buildTask *gradle.Task
	if cfg.AppType == appTypeAPK {
		buildTask = gradleProject.GetTask("assemble")
	} else {
		buildTask = gradleProject.GetTask("bundle")
	}

	variants, err := buildTask.GetVariants()
	if err != nil {
		return Result{}, fmt.Errorf("failed to fetch variants: %s", err)
	}

	filteredVariants, err := filterVariants(cfg.Module, cfg.Variant, variants)
	if err != nil {
		return Result{}, fmt.Errorf("failed to find buildable variants: %s", err)
	}

	a.printVariants(variants, filteredVariants)

	started := time.Now()

	if err := a.executeGradleBuild(cfg, buildTask, filteredVariants); err != nil {
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
	if result.appType == appTypeAPK {
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
	if result.appType == appTypeAPK {
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

// CollectCache ...
func (a AndroidBuild) CollectCache(cfg Config) {
	a.logger.Println()
	a.logger.Infof("Collecting cache:")
	if warning := androidcache.Collect(cfg.ProjectLocation, cfg.CacheLevel, a.cmdFactory); warning != nil {
		a.logger.Warnf("%s", warning)
	}
	a.logger.Donef("Done")
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

func (a AndroidBuild) executeGradleBuild(cfg Config, buildTask *gradle.Task, variants gradle.Variants) error {
	args, err := shellquote.Split(cfg.Arguments)
	if err != nil {
		return fmt.Errorf("failed to parse arguments: %s", err)
	}

	a.logger.Infof("Run build:")
	buildCommand := buildTask.GetCommand(variants, args...)

	a.logger.Println()
	a.logger.Donef("$ " + buildCommand.PrintableCommandArgs())
	a.logger.Println()

	if err := buildCommand.Run(); err != nil {
		return fmt.Errorf("build task failed: %v", err)
	}

	return nil
}

func (a AndroidBuild) printVariants(variants, filteredVariants gradle.Variants) {
	a.logger.Println()
	a.logger.Infof("Variants:")

	for module, variants := range variants {
		a.logger.Printf("%s:", module)
		for _, variant := range variants {
			if sliceutil.IsStringInSlice(variant, filteredVariants[module]) {
				a.logger.Donef("✓ %s", variant)
				continue
			}
			a.logger.Printf("- %s", variant)
		}
	}
	a.logger.Println()
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

func filterVariants(module, variant string, variantsMap gradle.Variants) (gradle.Variants, error) {
	// if module set: drop all the other modules
	if module != "" {
		v, ok := variantsMap[module]
		if !ok {
			return nil, fmt.Errorf("module not found: %s", module)
		}
		variantsMap = gradle.Variants{module: v}
	}

	// if variant not set: use all variants, except utility ones
	if variant == "" {
		for module, variants := range variantsMap {
			variantsMap[module] = filterNonUtilityVariants(variants)
		}

		return variantsMap, nil
	}

	variants := separateVariants(variant)

	filteredVariants := gradle.Variants{}
	for _, variant := range variants {
		found := false
		for m, moduleVariants := range variantsMap {
			for _, v := range moduleVariants {
				if strings.EqualFold(v, variant) {
					filteredVariants[m] = append(filteredVariants[m], v)
					found = true
				}
			}
		}

		if !found {
			return nil, fmt.Errorf("variant: %s not found in any module", variant)
		}
	}

	return filteredVariants, nil
}

func filterNonUtilityVariants(variants []string) []string {
	var filteredVariants []string

	for _, v := range variants {
		shouldIgnore := false
		for _, suffix := range ignoredSuffixes {
			if strings.HasSuffix(v, suffix) {
				shouldIgnore = true
				break
			}
		}

		if !shouldIgnore {
			filteredVariants = append(filteredVariants, v)
		}
	}

	return filteredVariants
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

func separateVariants(variantsAsOneLine string) []string {
	variants := strings.Split(variantsAsOneLine, `\n`)

	for index, variant := range variants {
		variants[index] = strings.TrimSpace(variant)
	}

	return variants
}
