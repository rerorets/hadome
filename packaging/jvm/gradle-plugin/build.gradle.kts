// The Gradle plugin io.github.rerorets.hadome. The installer's sources are compiled in, so the
// plugin has no dependencies. Build a release as ../README.md says.
plugins {
    `java-gradle-plugin`
    id("com.gradle.plugin-publish") version "1.3.1"
}

group = "io.github.rerorets"
version = providers.gradleProperty("hadomeVersion").getOrElse("0.0.0")

// Runs on Java 17 and later, whatever JDK builds it.
tasks.withType<JavaCompile>().configureEach { options.release = 17 }

sourceSets.main {
    java.srcDir("../installer/src/main/java")
    resources.srcDir("../release") // the release's version and SHA256SUMS; see ../README.md
}

gradlePlugin {
    website = "https://github.com/rerorets/hadome"
    vcsUrl = "https://github.com/rerorets/hadome"
    plugins {
        create("hadome") {
            id = "io.github.rerorets.hadome"
            implementationClass = "io.github.rerorets.hadome.gradle.HadomePlugin"
            displayName = "hadome"
            description = "Reports the violations a change adds and the tests it weakens."
            tags = listOf("testing", "verification", "regression", "ci")
        }
    }
}
