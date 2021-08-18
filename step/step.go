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
	APKPathPattern  string `env:"apk_path_pattern"`
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

	APKPathPattern string
	AppPathPattern string

	Variant string
	Module  string

	AppType   AppType
	Arguments string

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
	appArtifacts []gradle.Artifact
	mappings     []gradle.Artifact
	appPatterns  []string // TODO: temporary
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
		APKPathPattern:  input.APKPathPattern,
		AppPathPattern:  input.AppPathPattern,
		Variant:         input.Variant,
		Module:          input.Module,
		AppType:         AppType(input.BuildType),
		Arguments:       input.Arguments,
		CacheLevel:      cache.Level(input.CacheLevel),
		DeployDir:       input.DeployDir,
	}, nil
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

	started := time.Now()

	if err := executeGradleBuild(cfg, buildTask, filteredVariants); err != nil {
		return Result{}, err
	}

	fmt.Println()
	log.Infof("Export Artifacts:")

	//
	// Config migration from apk_path_pattern to app_path_pattern
	// The apk_path_pattern is deprecated
	// New input: app_path_pattern
	// If the apk_path_pattern is used log a warning, and still use the deprecated apk_path_pattern and ignore the new app_path_pattern temporarily
	var appPatterns []string
	if strings.TrimSpace(cfg.APKPathPattern) != "" {
		log.Warnf(`Step input 'APK location pattern' (apk_path_pattern) is deprecated and will be removed on 20 August 2019,
use 'App artifact (.apk, .aab) location pattern' (app_path_pattern) instead.`)
		fmt.Println()
		log.Infof(`'APK location pattern' (apk_path_pattern) is used, 'App artifact (.apk, .aab) location pattern' (app_path_pattern) is ignored.
Use 'App artifact (.apk, .aab) location pattern' and set 'APK location pattern' to empty value.`)
		fmt.Println()

		appPatterns = strings.Split(cfg.APKPathPattern, "\n")
	} else {
		appPatterns = strings.Split(cfg.AppPathPattern, "\n")
	}

	appArtifacts, err := getArtifacts(gradleProject, started, appPatterns, false)
	if err != nil {
		return Result{}, fmt.Errorf("failed to find app artifacts: %v", err)
	}

	mappings, err := getArtifacts(gradleProject, started, []string{mappingFilePattern}, true)
	if err != nil {
		return Result{}, fmt.Errorf("failed to find mapping files, error: %v", err)
	}

	return Result{
		appArtifacts: appArtifacts,
		mappings:     mappings,
		appPatterns:  appPatterns,
	}, nil
}

func (a AndroidBuild) Export(result Result, cfg Config) error {
	var artPaths []string
	for _, a := range result.appArtifacts {
		artPaths = append(artPaths, a.Name)
	}

	log.Donef("Used patterns for generated artifact search:")
	log.Printf(strings.Join(result.appPatterns, "\n"))
	fmt.Println()
	log.Donef("Found app artifacts:")
	log.Printf(strings.Join(artPaths, "\n"))
	fmt.Println()

	log.Donef("Exporting artifacts with the selected (%s) app type", cfg.AppType)
	// Filter appArtifacts by build type
	var filteredArtifacts []gradle.Artifact
	for _, a := range result.appArtifacts {
		if filepath.Ext(a.Path) == fmt.Sprintf(".%s", cfg.AppType) {
			filteredArtifacts = append(filteredArtifacts, a)
		}
	}

	if len(filteredArtifacts) == 0 {
		log.Warnf("No app artifacts found with patterns: %s", strings.Join(result.appPatterns, ", "))
		log.Warnf("If you have changed default APK, AAB export path in your gradle files then you might need to change AppPathPattern accordingly.")
		return nil
	}

	exportedArtifactPaths, err := exportArtifacts(filteredArtifacts, cfg.DeployDir)
	if err != nil {
		return fmt.Errorf("failed to export artifact: %v", err)
	}

	if len(exportedArtifactPaths) == 0 {
		return fmt.Errorf("could not export any app artifacts")
	}

	lastExportedArtifact := exportedArtifactPaths[len(exportedArtifactPaths)-1]

	fmt.Println()

	// Use the correct env key for the selected build type
	var envKey string
	if cfg.AppType == AppTypeAPK {
		envKey = apkEnvKey
	} else {
		envKey = aabEnvKey
	}
	if err := tools.ExportEnvironmentWithEnvman(envKey, lastExportedArtifact); err != nil {
		return fmt.Errorf("failed to export environment variable: %s", envKey)
	}
	log.Printf("  Env    [ $%s = $BITRISE_DEPLOY_DIR/%s ]", envKey, filepath.Base(lastExportedArtifact))

	var paths, sep string
	for _, path := range exportedArtifactPaths {
		paths += sep + "$BITRISE_DEPLOY_DIR/" + filepath.Base(path)
		sep = "| \\\n" + strings.Repeat(" ", 11)
	}

	// Use the correct env key for the selected build type
	if cfg.AppType == AppTypeAPK {
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

	if len(result.mappings) == 0 {
		log.Printf("No mapping files found with pattern: %s", mappingFilePattern)
		log.Printf("You might have changed default mapping file export path in your gradle files or obfuscation is not enabled in your project.")
		return nil // TODO
	}

	exportedArtifactPaths, err = exportArtifacts(result.mappings, cfg.DeployDir)
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

	fmt.Println()
	log.Infof("Collecting cache:")
	if warning := androidcache.Collect(cfg.ProjectLocation, cfg.CacheLevel); warning != nil {
		log.Warnf("%s", warning)
	}

	return nil
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
			log.Warnf("No appArtifacts found with patterns: %s that has modification time after: %s", strings.Join(patterns, ", "), started)
			log.Warnf("Retrying without modtime check....")
			fmt.Println()
			return getArtifacts(gradleProject, time.Time{}, patterns, includeModule)
		}
		log.Warnf("No appArtifacts found with pattern: %s without modtime check", strings.Join(patterns, ", "))
	}
	return
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
