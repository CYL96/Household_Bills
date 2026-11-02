{ pkgs ? import <nixpkgs> {
    config.allowUnfree = true;
    config.android_sdk.accept_license = true;
  }
}:

let
  buildToolsVersion = "35.0.0";
  androidSdk = (pkgs.androidenv.composeAndroidPackages {
    platformVersions = [ "35" ];
    buildToolsVersions = [ buildToolsVersion ];
    toolsVersion = null;
    includeCmake = false;
    includeEmulator = true;
    includeSystemImages = true;
    systemImageTypes = [ "google_apis" ];
    abiVersions = [ "x86_64" ];
  }).androidsdk;

  jdk = pkgs.jdk17;
  androidHome = "${androidSdk}/libexec/android-sdk";
  aapt2 = "${androidHome}/build-tools/${buildToolsVersion}/aapt2";
in
pkgs.mkShell {
  name = "household-bills-android";
  packages = [ androidSdk jdk ];

  ANDROID_HOME = androidHome;
  ANDROID_SDK_ROOT = androidHome;
  JAVA_HOME = jdk.home;

  shellHook = ''
    # Keep any existing Gradle options and append the Nix-native aapt2 override last,
    # so the Android Gradle Plugin does not download an FHS-dependent aapt2.
    export GRADLE_OPTS="''${GRADLE_OPTS:+$GRADLE_OPTS }-Dorg.gradle.project.android.aapt2FromMavenOverride=${aapt2}"
  '';
}
