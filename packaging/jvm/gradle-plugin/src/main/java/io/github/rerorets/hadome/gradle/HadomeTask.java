package io.github.rerorets.hadome.gradle;

import io.github.rerorets.hadome.installer.Installer;
import java.nio.file.Path;
import org.gradle.api.DefaultTask;
import org.gradle.api.GradleException;
import org.gradle.api.file.DirectoryProperty;
import org.gradle.api.provider.ListProperty;
import org.gradle.api.tasks.Input;
import org.gradle.api.tasks.Internal;
import org.gradle.api.tasks.TaskAction;
import org.gradle.api.tasks.options.Option;

/** Runs hadome with arguments; any exit code but 0 fails the build with what it means. */
public abstract class HadomeTask extends DefaultTask {
  /** The arguments, `check` by default. */
  @Input
  public abstract ListProperty<String> getArguments();

  /** Where hadome runs: the root project's directory by default. */
  @Internal
  public abstract DirectoryProperty getWorkingDir();

  /** Where downloaded binaries are kept, by version and platform. */
  @Internal
  public abstract DirectoryProperty getCache();

  /** Sets the arguments from the command line: gradle hadomeCheck --args="check --base origin/main". */
  @Option(option = "args", description = "Arguments for hadome, such as \"check --base origin/main\".")
  public void setArgs(String args) {
    getArguments().set(java.util.Arrays.asList(args.trim().split("\\s+")));
  }

  /** Installs the binary if needed and runs it. */
  @TaskAction
  public void run() throws Exception {
    Installer installer = new Installer();
    Path binary = installer.install(getCache().get().getAsFile().toPath());
    int code = Installer.run(binary, getArguments().get(), getWorkingDir().get().getAsFile().toPath(), System.out, System.err);
    switch (code) {
      case 0 -> { }
      case 1 -> throw new GradleException("hadome found regressions (exit code 1); see above");
      case 3 -> throw new GradleException("hadome needs a person's review (exit code 3); see above");
      default -> throw new GradleException("hadome could not run the check (exit code " + code + "); see above");
    }
  }
}
