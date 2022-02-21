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
