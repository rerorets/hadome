package io.github.rerorets.hadome.gradle;

import java.util.List;
import org.gradle.api.Plugin;
import org.gradle.api.Project;

/** Adds the hadomeCheck task, which runs `hadome check` in the root project's directory. */
public final class HadomePlugin implements Plugin<Project> {
  /** Registers hadomeCheck, caching downloaded binaries under the Gradle user home. */
  @Override
  public void apply(Project project) {
    project.getTasks().register("hadomeCheck", HadomeTask.class, task -> {
      task.setGroup("verification");
      task.setDescription("Fails on regressions in code and tests compared with the base branch (hadome check).");
      task.getArguments().convention(List.of("check"));
      task.getWorkingDir().convention(project.getRootProject().getLayout().getProjectDirectory());
      task.getCache().convention(project.getLayout().dir(project.provider(() ->
          project.getGradle().getGradleUserHomeDir().toPath().resolve("caches").resolve("hadome").toFile())));
      task.getOutputs().upToDateWhen(t -> false); // it judges the working tree, which Gradle does not track
    });
  }
}
