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
	AppType        AppType
	Arguments      string

	CacheLevel cache.Level
	DeployDir  string
}

type AppType string

const (
	AppTypeAPK = AppType("apk")
	AppTypeAAB = AppType("aab")
)

var (
	ignoredSuffixes = [...]string{"Classes", "Resources", "UnitTestClasses", "AndroidTestClasses", "AndroidTestResources"}
)

const (
	apkEnvKey     = "BITRISE_APK_PATH"
	apkListEnvKey = "BITRISE_APK_PATH_LIST"

	aabEnvKey     = "BITRISE_AAB_PATH"
	aabListEnvKey = "BITRISE_AAB_PATH_LIST"

	mappingFileEnvKey  = "BITRISE_MAPPING_PATH"
	mappingFilePattern = "*build/*/mapping.txt"
)

type Result struct {
	appFiles     []gradle.Artifact
	appType      AppType
	mappingFiles []gradle.Artifact
}

type ExportConfig struct {
	DeployDir      string
	AppPathPattern string
}

type InputParser interface {
	Parse() (Input, error)
}

type envInputParser struct{}

func NewInputParser() InputParser {
	return envInputParser{}
}

func (envInputParser) Parse() (Input, error) {
	var i Input
	if err := stepconf.Parse(&i); err != nil {
		return Input{}, err
	}
	return i, nil
}

type AndroidBuild struct {
	stepInputParser InputParser
}

// TODO: bloat
func NewAndroidBuild(stepInputParser InputParser) *AndroidBuild {
	return &AndroidBuild{stepInputParser: stepInputParser}
}

func (a AndroidBuild) ProcessConfig() (Config, error) {
	input, err := a.stepInputParser.Parse()
	if err != nil {
		return Config{}, nil
	}
	stepconf.Print(input)
	return Config{
		ProjectLocation: input.ProjectLocation,
		AppPathPattern:  input.AppPathPattern,
		Variant:         input.Variant,
		Module:          input.Module,
		AppType:         AppType(input.BuildType),
		Arguments:       input.Arguments,
		CacheLevel:      cache.Level(input.CacheLevel),
		DeployDir:       input.DeployDir,
	}, nil
}

func (a AndroidBuild) Run(cfg Config) (Result, error) {
	gradleProject, err := gradle.NewProject(cfg.ProjectLocation)
	if err != nil {
		return Result{}, fmt.Errorf("failed to open Gradle project: %s", err)
	}

	var buildTask *gradle.Task
	if cfg.AppType == AppTypeAPK {
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

	printVariants(variants, filteredVariants)

	started := time.Now()

	if err := executeGradleBuild(cfg, buildTask, filteredVariants); err != nil {
		return Result{}, err
	}

	fmt.Println()
	log.Infof("Export Artifacts:")

	var appPathPatterns = strings.Split(cfg.AppPathPattern, "\n")
	appArtifacts, err := getArtifacts(gradleProject, started, appPathPatterns, false)
	if err != nil {
		return Result{}, fmt.Errorf("failed to find app artifacts: %v", err)
	}

	mappings, err := getArtifacts(gradleProject, started, []string{mappingFilePattern}, true)
	if err != nil {
		return Result{}, fmt.Errorf("failed to find mapping files, error: %v", err)
	}

	printAppSearchInfo(appArtifacts, appPathPatterns)

	log.Donef("Exporting artifacts with the selected app type: %s", cfg.AppType)
	// Filter appFiles by build type
	var filteredArtifacts []gradle.Artifact
	for _, a := range appArtifacts {
		if filepath.Ext(a.Path) == fmt.Sprintf(".%s", cfg.AppType) {
			filteredArtifacts = append(filteredArtifacts, a)
		}
	}

	if len(filteredArtifacts) == 0 {
		log.Warnf("No app artifacts found with patterns:\n%s", cfg.AppPathPattern)
		log.Warnf("If you have changed default APK, AAB export path in your gradle files then you might need to change app_path_pattern accordingly.")
	}

	return Result{
		appFiles:     filteredArtifacts,
		appType:      cfg.AppType,
		mappingFiles: mappings,
	}, nil
}

func (a AndroidBuild) Export(result Result, exportCfg ExportConfig) error {
	exportedArtifactPaths, err := exportArtifacts(result.appFiles, exportCfg.DeployDir)
	if err != nil {
		return fmt.Errorf("failed to export artifact: %v", err)
	}

	if len(exportedArtifactPaths) == 0 {
		return fmt.Errorf("could not export any app artifacts")
	}

	lastExportedArtifact := exportedArtifactPaths[len(exportedArtifactPaths)-1]

	// Use the correct env key for the selected build type
	var envKey string
	if result.appType == AppTypeAPK {
		envKey = apkEnvKey
	} else {
		envKey = aabEnvKey
	}
	if err := tools.ExportEnvironmentWithEnvman(envKey, lastExportedArtifact); err != nil {
		return fmt.Errorf("failed to export environment variable: %s", envKey)
	}
	fmt.Println()
	log.Printf("  Env    [ $%s = $BITRISE_DEPLOY_DIR/%s ]", envKey, filepath.Base(lastExportedArtifact))

	var paths, sep string
	for _, path := range exportedArtifactPaths {
		paths += sep + "$BITRISE_DEPLOY_DIR/" + filepath.Base(path)
		sep = "| \\\n" + strings.Repeat(" ", 11)
	}

	// Use the correct env key for the selected build type
	if result.appType == AppTypeAPK {
		envKey = apkListEnvKey
	} else {
		envKey = aabListEnvKey
	}
	if err := tools.ExportEnvironmentWithEnvman(envKey, strings.Join(exportedArtifactPaths, "|")); err != nil {
		return fmt.Errorf("failed to export environment variable: %s", envKey)
	}
	log.Printf("  Env    [ $%s = %s ]", envKey, paths)

	fmt.Println()

	log.Infof("Export mapping files:")
	fmt.Println()

	if len(result.mappingFiles) == 0 {
		log.Printf("No mapping files found with pattern: %s", mappingFilePattern)
		log.Printf("You might have changed default mapping file export path in your gradle files or obfuscation is not enabled in your project.")
		return nil
	}

	exportedArtifactPaths, err = exportArtifacts(result.mappingFiles, exportCfg.DeployDir)
	if err != nil {
		return fmt.Errorf("failed to export artifact: %v", err)
	}

	if len(exportedArtifactPaths) == 0 {
		return fmt.Errorf("could not export any mapping.txt")
	}

	lastExportedArtifact = exportedArtifactPaths[len(exportedArtifactPaths)-1]

	fmt.Println()
	if err := tools.ExportEnvironmentWithEnvman(mappingFileEnvKey, lastExportedArtifact); err != nil {
		return fmt.Errorf("failed to export environment variable: %s", mappingFileEnvKey)
	}
	log.Printf("  Env    [ $%s = $BITRISE_DEPLOY_DIR/%s ]", mappingFileEnvKey, filepath.Base(lastExportedArtifact))

	return nil
}

func (a AndroidBuild) CollectCache(cfg Config) {
	fmt.Println()
	log.Infof("Collecting cache:")
	if warning := androidcache.Collect(cfg.ProjectLocation, cfg.CacheLevel); warning != nil {
		log.Warnf("%s", warning)
	}
}

func getArtifacts(gradleProject gradle.Project, started time.Time, patterns []string, includeModule bool) (artifacts []gradle.Artifact, err error) {
	for _, pattern := range patterns {
		afs, err := gradleProject.FindArtifacts(started, pattern, includeModule)
		if err != nil {
			log.Warnf("Failed to find artifact with pattern ( %s ), error: %s", pattern, err)
			continue
		}
		artifacts = append(artifacts, afs...)
	}

	if len(artifacts) == 0 {
		if !started.IsZero() {
			log.Warnf("No app files found with patterns: %s that has modification time after: %s", strings.Join(patterns, ", "), started)
			log.Warnf("Retrying without modtime check....")
			fmt.Println()
			return getArtifacts(gradleProject, time.Time{}, patterns, includeModule)
		}
		log.Warnf("No app files found with pattern: %s without modtime check", strings.Join(patterns, ", "))
	}
	return
}

func executeGradleBuild(cfg Config, buildTask *gradle.Task, variants gradle.Variants) error {
	args, err := shellquote.Split(cfg.Arguments)
	if err != nil {
		return fmt.Errorf("failed to parse arguments: %s", err)
	}

	log.Infof("Run build:")
	buildCommand := buildTask.GetCommand(variants, args...)

	fmt.Println()
	log.Donef("$ " + buildCommand.PrintableCommandArgs())
	fmt.Println()

	if err := buildCommand.Run(); err != nil {
		return fmt.Errorf("build task failed: %v", err)
	}

	return nil
}

func printVariants(variants, filteredVariants gradle.Variants) {
	fmt.Println()
	log.Infof("Variants:")

	for module, variants := range variants {
		log.Printf("%s:", module)
		for _, variant := range variants {
			if sliceutil.IsStringInSlice(variant, filteredVariants[module]) {
				log.Donef("✓ %s", variant)
				continue
			}
			log.Printf("- %s", variant)
		}
	}
	fmt.Println()
}

func printAppSearchInfo(appArtifacts []gradle.Artifact, appPathPatterns []string) {
	var artPaths []string
	for _, a := range appArtifacts {
		artPaths = append(artPaths, a.Name)
	}

	log.Donef("Used patterns for generated artifact search:")
	log.Printf(strings.Join(appPathPatterns, "\n"))
	fmt.Println()
	log.Donef("Found app artifacts:")
	log.Printf(strings.Join(artPaths, "\n"))
	fmt.Println()
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

func exportArtifacts(artifacts []gradle.Artifact, deployDir string) ([]string, error) {
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

		log.Printf("  Export [ %s => $BITRISE_DEPLOY_DIR/%s ]", artifactName, artifact.Name)

		if err := artifact.Export(deployDir); err != nil {
			log.Warnf("failed to export artifact (%s), error: %v", artifact.Path, err)
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
