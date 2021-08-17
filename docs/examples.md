### Examples

Build an APK from the debug variant:

```yaml
- android-build:
    inputs:
    - variant: debug
    - app_type: apk
```

Build a release AAB:

```yaml
- android-build:
    inputs:
    - variant: release
    - app_type: aab
```
