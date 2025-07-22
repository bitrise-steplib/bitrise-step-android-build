# Android Build

[![Step changelog](https://shields.io/github/v/release/bitrise-steplib/bitrise-step-android-build?include_prereleases&label=changelog&color=blueviolet)](https://github.com/bitrise-steplib/bitrise-step-android-build/releases)

Builds your Android project with Gradle.

<details>
<summary>Description</summary>


The Step builds your Android project on Bitrise with Gradle commands: it installs all dependencies that are listed in the project's `build.gradle` file, and builds and exports either an APK or an AAB.
Once the file is exported, it is available for other Steps in your Workflow.

You can select the module and the variant you want to use for the build.

### Configuring the Step

1. Make sure the **Project Location** input points to the root directory of your Android project.
1. In the **Module** input, set the module that you want to build.

   You can find the available modules in Android Studio.

1. In the **Variant** input, set the variant that you want to build.

   You can find the available variants in Android Studio.

1. In the **Build type** input, select the file type you want to build.

   The options are:
   - `apk`
   - `aab`

1. In the **Options** input group, you can set more advanced configuration options for the Step:

   - In the **App artifact (.apk, .aab) location pattern** input, you can tell the Step where to look for the APK or AAB files in your project to export them.
   For the vast majority of Android projects, the default values do NOT need to be changed.

   - In the **Additional Gradle Arguments**, you can add additional command line arguments to the Gradle task. Read more about [Gradle's Command Line Interface](https://docs.gradle.org/current/userguide/command_line_interface.html).

   - The **Set the level of cache** input allows you to set what will be cached during the build: everything, dependencies only, or nothing.

### Troubleshooting

Be aware that an APK or AAB built by the Step is still unsigned: code signing is performed either in Gradle itself or by other Steps. To be able to deploy your APK or AAB to an online store, you need code signing.

If you want to build a custom module or variant, always check that the value you set in the respective input is correct. A typo means your build will fail; if the module or variant does not exist in Android Studio, the build will fail.

### Useful links

- [Getting started with Android apps](https://devcenter.bitrise.io/getting-started/getting-started-with-android-apps/)
- [Deploying Android apps](https://devcenter.bitrise.io/deploy/android-deploy/deploying-android-apps/)
- [Generating and deploying Android app bundles](https://devcenter.bitrise.io/deploy/android-deploy/generating-and-deploying-android-app-bundles/)
- [Gradle's Command Line Interface](https://docs.gradle.org/current/userguide/command_line_interface.html)

### Related Steps

- [Gradle Runner](https://www.bitrise.io/integrations/steps/gradle-runner)
- [Android Sign](https://www.bitrise.io/integrations/steps/sign-apk)
- [Install missing Android SDK components](https://www.bitrise.io/integrations/steps/install-missing-android-tools)
</details>

## 🧩 Get started

Add this step directly to your workflow in the [Bitrise Workflow Editor](https://devcenter.bitrise.io/steps-and-workflows/steps-and-workflows-index/).

You can also run this step directly with [Bitrise CLI](https://github.com/bitrise-io/bitrise).

### Examples

Build an APK from the debug variant:

```yaml
- android-build:
    inputs:
    - variant: debug
    - build_type: apk
```

Build a release AAB:

```yaml
- android-build:
    inputs:
    - variant: release
    - build_type: aab
```


## ⚙️ Configuration

<details>
<summary>Inputs</summary>

| Key | Description | Flags | Default |
| --- | --- | --- | --- |
| `project_location` | The root directory of your Android project. For example, where your root build gradle file exist (also gradlew, settings.gradle, and so on) | required | `$BITRISE_SOURCE_DIR` |
| `module` | Set the module that you want to build. To see your available modules, please open your project in Android Studio and go in [Project Structure] and see the list on the left.  |  |  |
| `variant` | Set the build variants you want to create. To see your available variants,  open your project in Android Studio and go in [Project Structure] -> variants section.  This input also accepts multiple variants, separated by a line break.  |  |  |
| `build_type` | Set the build type that you want to build.  | required | `apk` |
| `app_path_pattern` | Will find the APK or AAB files - depending on the **Build type** input - with the given pattern.<br/> Separate patterns with a newline. **Note**<br/> The Step will export only the selected artifact type even if the filter would accept other artifact types as well.  | required | `*/build/outputs/apk/*.apk */build/outputs/bundle/*.aab` |
| `arguments` | Extra arguments passed to the gradle task |  |  |
</details>

<details>
<summary>Outputs</summary>

| Environment Variable | Description |
| --- | --- |
| `BITRISE_APK_PATH` | This output will include the path of the generated APK after filtering based on the filter inputs. If the build generates more than one APK which fulfills the filter inputs, this output will contain the last one's path. |
| `BITRISE_APK_PATH_LIST` | This output will include the paths of the generated APKs after filtering based on the filter inputs. The paths are separated with `\|` character, for example, `app-armeabi-v7a-debug.apk\|app-mips-debug.apk\|app-x86-debug.apk` |
| `BITRISE_AAB_PATH` | This output will include the path of the generated AAB after filtering based on the filter inputs. If the build generates more than one AAB which fulfills the filter inputs, this output will contain the last one's path. |
| `BITRISE_AAB_PATH_LIST` | This output will include the paths of the generated AABs after filtering based on the filter inputs. The paths are separated with `\|` character, for example, `app--debug.aab\|app-mips-debug.aab` |
| `BITRISE_MAPPING_PATH` | This output will include the path of the generated mapping.txt. If more than one mapping.txt exist in the project, this output will contain the last one's path. |
</details>

## 🙋 Contributing

We welcome [pull requests](https://github.com/bitrise-steplib/bitrise-step-android-build/pulls) and [issues](https://github.com/bitrise-steplib/bitrise-step-android-build/issues) against this repository.

For pull requests, work on your changes in a forked repository and use the Bitrise CLI to [run step tests locally](https://devcenter.bitrise.io/bitrise-cli/run-your-first-build/).

Learn more about developing steps:

- [Create your own step](https://devcenter.bitrise.io/contributors/create-your-own-step/)
- [Testing your Step](https://devcenter.bitrise.io/contributors/testing-and-versioning-your-steps/)
