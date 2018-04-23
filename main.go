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
	apkEnvKey          = "BITRISE_APK_PATH"
	apkListEnvKey      = "BITRISE_APK_PATH_LIST"
	mappingFileEnvKey  = "BITRISE_MAPPING_PATH"
	mappingFilePattern = "*build/*/mapping.txt"
)

// Configs ...
type Configs struct {
	ProjectLocation string `env:"project_location,dir"`
	APKPathPattern  string `env:"apk_path_pattern"`
	Variant         string `env:"variant"`
	Module          string `env:"module"`
	Arguments       string `env:"arguments"`
	CacheLevel      string `env:"cache_level,opt[none,only_deps,all]"`
}

func getArtifacts(gradleProject gradle.Project, started time.Time, pattern string, includeModule bool) (artifacts []gradle.Artifact, err error) {
	artifacts, err = gradleProject.FindArtifacts(started, pattern, includeModule)
	if err != nil {
		return
	}
	if len(artifacts) == 0 {
		if !started.IsZero() {
			log.Warnf("No artifacts found with pattern: %s that has modification time after: %s", pattern, started)
			log.Warnf("Retrying without modtime check....")
			fmt.Println()
			return getArtifacts(gradleProject, time.Time{}, pattern, includeModule)
		}
		log.Warnf("No artifacts found with pattern: %s without modtime check", pattern)
		log.Warnf("If you have changed default report export path in your gradle files then you might need to change ReportPathPattern accordingly.")
	}
	return
}

func exportArtifacts(artifacts []gradle.Artifact, deployDir string) ([]string, error) {
	var exportedArtifactPaths []string
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

		exportedArtifactPaths = append(exportedArtifactPaths, filepath.Join(deployDir, artifact.Name))
	}
	return exportedArtifactPaths, nil
}

func failf(f string, args ...interface{}) {
	log.Errorf(f, args...)
	os.Exit(1)
}

func main() {
	var config Configs

	if err := stepconf.Parse(&config); err != nil {
		failf("Couldn't create step config: %v\n", err)
	}

	stepconf.Print(config)

	deployDir := os.Getenv("BITRISE_DEPLOY_DIR")

	log.Printf("- Deploy dir: %s", deployDir)
	fmt.Println()

	gradleProject, err := gradle.NewProject(config.ProjectLocation)
	if err != nil {
		failf("Failed to open project, error: %s", err)
	}

	buildTask := gradleProject.
		GetTask("assemble")

	log.Infof("Variants:")
	fmt.Println()

	variants, err := buildTask.GetVariants()
	if err != nil {
		failf("Failed to fetch variants, error: %s", err)
	}

	filteredVariants := variants.Filter(config.Module, config.Variant)

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

	if len(filteredVariants) == 0 {
		if config.Variant != "" {
			if config.Module == "" {
				failf("Variant (%s) not found in any module", config.Variant)
			}
			failf("No variant matching for (%s) in module: [%s]", config.Variant, config.Module)
		}
		failf("Module not found: %s", config.Module)
	}

	started := time.Now()

	args, err := shellquote.Split(config.Arguments)
	if err != nil {
		failf("Failed to parse arguments, error: %s", err)
	}

	log.Infof("Run build:")
	if err := buildTask.Run(filteredVariants, args...); err != nil {
		failf("Build task failed, error: %v", err)
	}
	fmt.Println()

	log.Infof("Export APKs:")
	fmt.Println()

	apks, err := getArtifacts(gradleProject, started, config.APKPathPattern, false)
	if err != nil {
		failf("failed to find apks, error: %v", err)
	}

	if len(apks) == 0 {
		log.Warnf("No apks found with pattern: %s", config.APKPathPattern)
		log.Warnf("If you have changed default APK export path in your gradle files then you might need to change APKPathPattern accordingly.")
		os.Exit(0)
	}

	exportedArtifactPaths, err := exportArtifacts(apks, deployDir)
	if err != nil {
		failf("Failed to export artifact: %v", err)
	}

	if len(exportedArtifactPaths) == 0 {
		failf("Could not export any APKs")
	}

	lastExportedArtifact := exportedArtifactPaths[len(exportedArtifactPaths)-1]

	fmt.Println()
	if err := tools.ExportEnvironmentWithEnvman(apkEnvKey, lastExportedArtifact); err != nil {
		failf("Failed to export environment variable: %s", apkEnvKey)
	}
	log.Printf("  Env    [ $%s = $BITRISE_DEPLOY_DIR/%s ]", apkEnvKey, filepath.Base(lastExportedArtifact))

	var paths, sep string
	for _, path := range exportedArtifactPaths {
		paths += sep + "$BITRISE_DEPLOY_DIR/" + filepath.Base(path)
		sep = "| \\\n" + strings.Repeat(" ", 11)
	}

	if err := tools.ExportEnvironmentWithEnvman(apkEnvKey, lastExportedArtifact); err != nil {
		failf("Failed to export environment variable: %s", apkEnvKey)
	}
	log.Printf("  Env    [ $%s = %s ]", apkListEnvKey, paths)

	fmt.Println()
	log.Infof("Collecting cache:")
	if warning := cache.Collect(config.ProjectLocation, cache.Level(config.CacheLevel)); warning != nil {
		log.Warnf("%s", warning)
	}
	log.Donef("  Done")
	fmt.Println()

	log.Infof("Export mapping files:")
	fmt.Println()

	mappings, err := getArtifacts(gradleProject, started, mappingFilePattern, true)
	if err != nil {
		log.Warnf("Failed to find mapping files, error: %v", err)
		return
	}

	if len(mappings) == 0 {
		log.Printf("No mapping files found with pattern: %s", mappingFilePattern)
		log.Printf("You might have changed default mapping file export path in your gradle files or obfuscation is not enabled in your project.")
		os.Exit(0)
	}

	exportedArtifactPaths, err = exportArtifacts(mappings, deployDir)
	if err != nil {
		failf("Failed to export artifact: %v", err)
	}

	if len(exportedArtifactPaths) == 0 {
		failf("Could not export any mapping.txt")
	}

	lastExportedArtifact = exportedArtifactPaths[len(exportedArtifactPaths)-1]

	fmt.Println()
	if err := tools.ExportEnvironmentWithEnvman(mappingFileEnvKey, lastExportedArtifact); err != nil {
		failf("Failed to export environment variable: %s", mappingFileEnvKey)
	}
	log.Printf("  Env    [ $%s = $BITRISE_DEPLOY_DIR/%s ]", mappingFileEnvKey, filepath.Base(lastExportedArtifact))
}
