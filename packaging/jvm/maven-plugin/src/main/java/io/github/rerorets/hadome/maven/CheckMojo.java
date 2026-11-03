package io.github.rerorets.hadome.maven;

import io.github.rerorets.hadome.installer.Installer;
import java.io.File;
import java.nio.file.Path;
import java.util.Arrays;
import org.apache.maven.plugin.AbstractMojo;
import org.apache.maven.plugin.MojoExecutionException;
import org.apache.maven.plugin.MojoFailureException;
import org.apache.maven.plugins.annotations.Mojo;
import org.apache.maven.plugins.annotations.Parameter;

/**
 * Runs `hadome check` (or other arguments) in the directory Maven runs in; any exit code but 0
 * fails the build with what it means. Run it as mvn hadome:check.
 */
@Mojo(name = "check", requiresProject = false, threadSafe = true)
public final class CheckMojo extends AbstractMojo {
  /** The arguments, `check` by default: -Dhadome.args="check --base origin/main". */
  @Parameter(property = "hadome.args", defaultValue = "check")
  private String args;

  /** Where hadome runs: the directory Maven was started in, which is the repository root for a build of the whole project. */
  @Parameter(property = "hadome.dir", defaultValue = "${session.executionRootDirectory}")
  private File dir;

  /** Where downloaded binaries are kept, by version and platform. */
  @Parameter(property = "hadome.cache", defaultValue = "${settings.localRepository}/../hadome")
  private File cache;

  /** Installs the binary if needed and runs it. */
  @Override
  public void execute() throws MojoExecutionException, MojoFailureException {
    int code;
    try {
      Installer installer = new Installer();
      Path binary = installer.install(cache.toPath().normalize());
      code = Installer.run(binary, Arrays.asList(args.trim().split("\\s+")), dir.toPath(), System.out, System.err);
    } catch (Exception e) {
      throw new MojoExecutionException("hadome could not be installed or run: " + e.getMessage(), e);
    }
    switch (code) {
      case 0 -> { }
      case 1 -> throw new MojoFailureException("hadome found regressions (exit code 1); see above");
      case 3 -> throw new MojoFailureException("hadome needs a person's review (exit code 3); see above");
      default -> throw new MojoFailureException("hadome could not run the check (exit code " + code + "); see above");
    }
  }
}
