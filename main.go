package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-io/go-utils/pathutil"
	"github.com/bitrise-io/go-utils/sliceutil"
	"github.com/bitrise-steplib/bitrise-step-android-unit-test/cache"
	"github.com/bitrise-tools/go-android/gradle"
	"github.com/bitrise-tools/go-steputils/stepconf"
	"github.com/bitrise-tools/go-steputils/tools"
	shellquote "github.com/kballard/go-shellquote"
)

const (
	apkEnvKey     = "BITRISE_APK_PATH"
	apkListEnvKey = "BITRISE_APK_PATH_LIST"

	aabEnvKey     = "BITRISE_AAB_PATH"
	aabListEnvKey = "BITRISE_AAB_PATH_LIST"

	mappingFileEnvKey  = "BITRISE_MAPPING_PATH"
	mappingFilePattern = "*build/*/mapping.txt"
)

// Configs ...
type Configs struct {
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
			log.Warnf("No artifacts found with patterns: %s that has modification time after: %s", strings.Join(patterns, ", "), started)
			log.Warnf("Retrying without modtime check....")
			fmt.Println()
			return getArtifacts(gradleProject, time.Time{}, patterns, includeModule)
		}
		log.Warnf("No artifacts found with pattern: %s without modtime check", strings.Join(patterns, ", "))
	}
	return
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

func filterVariants(module, variant string, variantsMap gradle.Variants) (gradle.Variants, error) {
	// if module set: drop all the other modules
	if module != "" {
		v, ok := variantsMap[module]
		if !ok {
			return nil, fmt.Errorf("module not found: %s", module)
		}
		variantsMap = gradle.Variants{module: v}
	}

	// if variant not set: use all variants
	if variant == "" {
		return variantsMap, nil
	}

	filteredVariants := gradle.Variants{}
	for m, variants := range variantsMap {
		for _, v := range variants {
			if strings.ToLower(v) == strings.ToLower(variant) {
				filteredVariants[m] = append(filteredVariants[m], v)
			}
		}
	}

	if len(filteredVariants) == 0 {
		return nil, fmt.Errorf("variant: %s not found in any module", variant)
	}
	return filteredVariants, nil
}

func mainE(config Configs, appPatterns []string) error {
	gradleProject, err := gradle.NewProject(config.ProjectLocation)
	if err != nil {
		return fmt.Errorf("Failed to open project, error: %s", err)
	}

	var buildTask *gradle.Task
	if config.BuildType == "apk" {
		buildTask = gradleProject.GetTask("assemble")
	} else {
		buildTask = gradleProject.GetTask("bundle")
	}

	log.Infof("Variants:")
	fmt.Println()

	variants, err := buildTask.GetVariants()
	if err != nil {
		return fmt.Errorf("Failed to fetch variants, error: %s", err)
	}

	filteredVariants, err := filterVariants(config.Module, config.Variant, variants)
	if err != nil {
		return fmt.Errorf("Failed to find buildable variants, error: %s", err)
	}

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

	args, err := shellquote.Split(config.Arguments)
	if err != nil {
		return fmt.Errorf("Failed to parse arguments, error: %s", err)
	}

	log.Infof("Run build:")
	buildCommand := buildTask.GetCommand(filteredVariants, args...)

	fmt.Println()
	log.Donef("$ " + buildCommand.PrintableCommandArgs())
	fmt.Println()

	if err := buildCommand.Run(); err != nil {
		return fmt.Errorf("Build task failed, error: %v", err)
	}

	fmt.Println()
	log.Infof("Export Artifacts:")

	artifacts, err := getArtifacts(gradleProject, started, appPatterns, false)
	if err != nil {
		return fmt.Errorf("failed to find artifacts, error: %v", err)
	}

	var artPaths []string
	for _, a := range artifacts {
		artPaths = append(artPaths, a.Name)
	}

	log.Donef("Used patterns for generated artifact search:")
	log.Printf(strings.Join(appPatterns, "\n"))
	fmt.Println()
	log.Donef("Found artifacts:")
	log.Printf(strings.Join(artPaths, "\n"))
	fmt.Println()

	log.Donef("Exporting artifacts with the selected (" + config.BuildType + ") build type")
	// Filter artifacts by build type
	var filteredArtifacts []gradle.Artifact
	for _, a := range artifacts {
		if filepath.Ext(a.Path) == "."+config.BuildType {
			filteredArtifacts = append(filteredArtifacts, a)
		}
	}

	if len(filteredArtifacts) == 0 {
		log.Warnf("No artifacts found with patterns: %s", strings.Join(appPatterns, ", "))
		log.Warnf("If you have changed default APK, AAB export path in your gradle files then you might need to change AppPathPattern accordingly.")
		return nil
	}

	exportedArtifactPaths, err := exportArtifacts(filteredArtifacts, config.DeployDir)
	if err != nil {
		return fmt.Errorf("Failed to export artifact: %v", err)
	}

	if len(exportedArtifactPaths) == 0 {
		return fmt.Errorf("Could not export any artifacts")
	}

	lastExportedArtifact := exportedArtifactPaths[len(exportedArtifactPaths)-1]

	fmt.Println()

	// Use the correct env key for the selected build type
	var envKey string
	if config.BuildType == "apk" {
		envKey = apkEnvKey
	} else {
		envKey = aabEnvKey
	}
	if err := tools.ExportEnvironmentWithEnvman(envKey, lastExportedArtifact); err != nil {
		return fmt.Errorf("Failed to export environment variable: %s", envKey)
	}
	log.Printf("  Env    [ $%s = $BITRISE_DEPLOY_DIR/%s ]", envKey, filepath.Base(lastExportedArtifact))

	var paths, sep string
	for _, path := range exportedArtifactPaths {
		paths += sep + "$BITRISE_DEPLOY_DIR/" + filepath.Base(path)
		sep = "| \\\n" + strings.Repeat(" ", 11)
	}

	// Use the correct env key for the selected build type
	if config.BuildType == "apk" {
		envKey = apkListEnvKey
	} else {
		envKey = aabListEnvKey
	}
	if err := tools.ExportEnvironmentWithEnvman(envKey, strings.Join(exportedArtifactPaths, "|")); err != nil {
		return fmt.Errorf("Failed to export environment variable: %s", envKey)
	}
	log.Printf("  Env    [ $%s = %s ]", envKey, paths)

	fmt.Println()

	log.Infof("Export mapping files:")
	fmt.Println()

	mappings, err := getArtifacts(gradleProject, started, []string{mappingFilePattern}, true)
	if err != nil {
		log.Warnf("Failed to find mapping files, error: %v", err)
		return nil
	}

	if len(mappings) == 0 {
		log.Printf("No mapping files found with pattern: %s", mappingFilePattern)
		log.Printf("You might have changed default mapping file export path in your gradle files or obfuscation is not enabled in your project.")
		return nil
	}

	exportedArtifactPaths, err = exportArtifacts(mappings, config.DeployDir)
	if err != nil {
		return fmt.Errorf("Failed to export artifact: %v", err)
	}

	if len(exportedArtifactPaths) == 0 {
		return fmt.Errorf("Could not export any mapping.txt")
	}

	lastExportedArtifact = exportedArtifactPaths[len(exportedArtifactPaths)-1]

	fmt.Println()
	if err := tools.ExportEnvironmentWithEnvman(mappingFileEnvKey, lastExportedArtifact); err != nil {
		return fmt.Errorf("Failed to export environment variable: %s", mappingFileEnvKey)
	}
	log.Printf("  Env    [ $%s = $BITRISE_DEPLOY_DIR/%s ]", mappingFileEnvKey, filepath.Base(lastExportedArtifact))
	return nil
}

func failf(s string, args ...interface{}) {
	log.Errorf(s, args...)
	os.Exit(1)
}

func main() {
	var config Configs

	if err := stepconf.Parse(&config); err != nil {
		failf("Couldn't create step config: %v", err)
	}

	stepconf.Print(config)
	fmt.Println()

	//
	// Config migration from apk_path_pattern to app_path_pattern
	// The apk_path_pattern is deprecated
	// New input: app_path_pattern
	// If the apk_path_pattern is used log a warning, and still use the deprecated apk_path_pattern and ignore the new app_path_pattern temporarily
	var appPatterns []string
	if strings.TrimSpace(config.APKPathPattern) != "" {
		log.Warnf(`Step input 'APK location pattern' (apk_path_pattern) is deprectad and will be removed soon,
use 'App artifact (.apk, .aab) location pattern' (app_path_pattern) instead.`)
		fmt.Println()
		log.Infof(`'APK location pattern' (apk_path_pattern) is used, 'App artifact (.apk, .aab) location pattern' (app_path_pattern) is ignored.
Use 'App artifact (.apk, .aab) location pattern' and set 'APK location pattern' to empty value.`)
		fmt.Println()

		appPatterns = append(strings.Split(config.APKPathPattern, "\n"))
	} else {
		appPatterns = append(strings.Split(config.AppPathPattern, "\n"))
	}

	if err := mainE(config, appPatterns); err != nil {
		failf("%s", err)
	}

	fmt.Println()
	log.Infof("Collecting cache:")
	if warning := cache.Collect(config.ProjectLocation, cache.Level(config.CacheLevel)); warning != nil {
		log.Warnf("%s", warning)
	}

	log.Donef("  Done")
}
