package io.github.rerorets.hadome.installer;

import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.net.URI;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.StandardCopyOption;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.HashMap;
import java.util.HexFormat;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.zip.GZIPInputStream;
import java.util.zip.ZipEntry;
import java.util.zip.ZipInputStream;

/**
 * Installs the hadome binary of the release this plugin was built for, and runs it. The Gradle and
 * Maven plugins share it.
 *
 * <p>The version and the release's SHA256SUMS are resources fixed when the plugin is built, so a
 * download is only ever accepted when its checksum matches what the release published.
 */
public final class Installer {
  /** Where releases are downloaded from; the system property hadome.downloadBaseUrl overrides it (mirrors, tests). */
  public static final String RELEASES = "https://github.com/rerorets/hadome/releases/download";

  private final String version;
  private final Map<String, String> sums;
  private final String base;

  /** An installer for the release whose version and checksums are built into this plugin. */
  public Installer() throws IOException {
    this(resource("hadome-version.txt").trim(), parseSums(resource("SHA256SUMS")),
        System.getProperty("hadome.downloadBaseUrl", RELEASES));
  }

  Installer(String version, Map<String, String> sums, String base) {
    this.version = version;
    this.sums = sums;
    this.base = base;
  }

  /** The release version this installer installs. */
  public String version() {
    return version;
  }

  /**
   * Returns the binary for this machine under cache, downloading and checking it first if it is not
   * there yet.
   */
  public Path install(Path cache) throws IOException {
    String platform = platform(System.getProperty("os.name"), System.getProperty("os.arch"));
    boolean windows = platform.startsWith("windows");
    String archive = "hadome_" + version + "_" + platform + (windows ? ".zip" : ".tar.gz");
    Path dir = cache.resolve(version).resolve(platform);
    Path binary = dir.resolve(windows ? "hadome.exe" : "hadome");
    if (Files.isExecutable(binary)) {
      return binary;
    }
    String want = sums.get(archive);
    if (want == null) {
      throw new IOException("this plugin has no checksum for " + archive + "; hadome " + version
          + " has no prebuilt binary for " + platform);
    }
    byte[] data = download(base + "/v" + version + "/" + archive);
    String got = sha256(data);
    if (!got.equals(want)) {
      throw new IOException("checksum mismatch for " + archive + ": want " + want + ", got " + got
          + "; the download was not used");
    }
    Files.createDirectories(dir);
    Path tmp = Files.createTempFile(dir, "hadome", ".tmp");
    Files.write(tmp, windows ? unzip(data, "hadome.exe") : untar(data, "hadome"));
    tmp.toFile().setExecutable(true);
    Files.move(tmp, binary, StandardCopyOption.REPLACE_EXISTING, StandardCopyOption.ATOMIC_MOVE);
    return binary;
  }

  /**
   * Runs the binary with the arguments in dir and returns its exit code. Its output is copied to out
   * and err rather than inherited: a build tool's daemon does not own the console the user sees.
   */
  public static int run(Path binary, List<String> args, Path dir, java.io.OutputStream out, java.io.OutputStream err)
      throws IOException, InterruptedException {
    List<String> command = new java.util.ArrayList<>();
    command.add(binary.toString());
    command.addAll(args);
    Process p = new ProcessBuilder(command).directory(dir.toFile()).redirectInput(ProcessBuilder.Redirect.INHERIT).start();
    Thread errors = new Thread(() -> copy(p.getErrorStream(), err));
    errors.start();
    copy(p.getInputStream(), out);
    errors.join();
    return p.waitFor();
  }

  private static void copy(InputStream in, java.io.OutputStream out) {
    try (in) {
      in.transferTo(out);
      out.flush();
    } catch (IOException e) {
      throw new java.io.UncheckedIOException(e);
    }
  }

  /** The release's name for a platform, such as linux_amd64. */
  static String platform(String osName, String osArch) throws IOException {
    String os = osName.toLowerCase(Locale.ROOT);
    String goos = os.contains("win") ? "windows" : os.contains("mac") || os.contains("darwin") ? "darwin"
        : os.contains("linux") ? "linux" : null;
    String arch = osArch.toLowerCase(Locale.ROOT);
    String goarch = arch.equals("amd64") || arch.equals("x86_64") ? "amd64"
        : arch.equals("aarch64") || arch.equals("arm64") ? "arm64" : null;
    if (goos == null || goarch == null) {
      throw new IOException("hadome has no prebuilt binary for " + osName + " " + osArch);
    }
    return goos + "_" + goarch;
  }

  static Map<String, String> parseSums(String text) {
    Map<String, String> out = new HashMap<>();
    for (String line : text.split("\n")) {
      String[] parts = line.trim().split("\\s+");
      if (parts.length == 2) {
        out.put(parts[1].replaceFirst("^\\*", ""), parts[0]);
      }
    }
    return out;
  }

  static String sha256(byte[] data) {
    try {
      return HexFormat.of().formatHex(MessageDigest.getInstance("SHA-256").digest(data));
    } catch (NoSuchAlgorithmException e) {
      throw new IllegalStateException("every Java runtime has SHA-256", e);
    }
  }

  private static String resource(String name) throws IOException {
    try (InputStream in = Installer.class.getResourceAsStream(name)) {
      if (in == null) {
        throw new IOException("this plugin was built without " + name + "; use a released version");
      }
      return new String(in.readAllBytes(), StandardCharsets.UTF_8);
    }
  }

  private static byte[] download(String url) throws IOException {
    try (InputStream in = URI.create(url).toURL().openStream()) {
      return in.readAllBytes();
    }
  }

  /** Returns the file called name from a gzipped tar archive. */
  static byte[] untar(byte[] archive, String name) throws IOException {
    try (InputStream in = new GZIPInputStream(new java.io.ByteArrayInputStream(archive))) {
      byte[] header = new byte[512];
      while (in.readNBytes(header, 0, 512) == 512 && header[0] != 0) {
        String entry = new String(header, 0, 100, StandardCharsets.US_ASCII).replaceAll("\0.*", "");
        long size = Long.parseLong(new String(header, 124, 12, StandardCharsets.US_ASCII).replaceAll("[\0 ].*", "").trim(), 8);
        byte[] body = in.readNBytes((int) size);
        in.skipNBytes((512 - size % 512) % 512);
        if (entry.equals(name) || entry.endsWith("/" + name)) {
          return body;
        }
      }
    }
    throw new IOException("the archive holds no " + name);
  }

  /** Returns the file called name from a zip archive. */
  static byte[] unzip(byte[] archive, String name) throws IOException {
    try (ZipInputStream in = new ZipInputStream(new java.io.ByteArrayInputStream(archive))) {
      for (ZipEntry e = in.getNextEntry(); e != null; e = in.getNextEntry()) {
        if (e.getName().equals(name) || e.getName().endsWith("/" + name)) {
          ByteArrayOutputStream out = new ByteArrayOutputStream();
          in.transferTo(out);
          return out.toByteArray();
        }
      }
    }
    throw new IOException("the archive holds no " + name);
  }
}
