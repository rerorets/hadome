# JVM plugins

The Gradle plugin (`io.github.rerorets.hadome`) and the Maven plugin (`io.github.rerorets:hadome-maven-plugin`) share the installer in `installer/`: it downloads the release's archive for the platform, checks it against the release's `SHA256SUMS`, and keeps the binary in the build tool's cache.

The version and the checksums are fixed when a plugin is built. Before building a release, write them into `release/` (ignored by git):

```sh
packaging/jvm/release.sh <version> <path to the release's SHA256SUMS>
```

Then `gradle build -PhadomeVersion=<version>` in `gradle-plugin/` and `mvn package -Drevision=<version>` in `maven-plugin/`. A plugin built without `release/` refuses to install anything.

`-Dhadome.downloadBaseUrl=<url>` makes the installer download from a mirror (or a local directory, `file:///…`) instead of GitHub; the checksums still apply.
