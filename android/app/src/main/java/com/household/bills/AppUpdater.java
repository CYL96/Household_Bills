package com.household.bills;

import android.content.Context;
import android.content.pm.PackageInfo;
import android.content.pm.PackageManager;
import android.content.pm.Signature;
import android.os.Build;
import android.os.Handler;
import android.os.Looper;
import android.os.SystemClock;

import org.json.JSONException;
import org.json.JSONObject;

import java.io.ByteArrayOutputStream;
import java.io.File;
import java.io.FileInputStream;
import java.io.FileOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.ConnectException;
import java.net.HttpURLConnection;
import java.net.MalformedURLException;
import java.net.SocketTimeoutException;
import java.net.URL;
import java.net.UnknownHostException;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.Locale;
import java.util.Set;
import java.util.TreeSet;
import java.util.regex.Pattern;

import javax.net.ssl.SSLException;

/**
 * Background update client for the saved ledger server.
 *
 * <p>Every task runs on a private worker thread; results are posted to the
 * main thread only while the task generation is current. Cancelling a task
 * disconnects its HTTP connection and invalidates its callbacks so a previous
 * origin can never update the UI.</p>
 *
 * <p>Only the fixed metadata endpoint and the exact fixed download path are
 * requested. Redirects, cookies, and server supplied URLs are never used.
 * TLS always uses the platform trust store.</p>
 */
final class AppUpdater {

    interface Listener {
        /** {@code metadata} is null when no update is published; error is null on success. */
        void onCheckFinished(Metadata metadata, String error);

        void onDownloadProgress(long downloaded, long totalBytes);

        void onDownloadFinished(File apkFile, Metadata metadata, String error);

        void onRevalidateFinished(File apkFile, String error);
    }

    /** Validated metadata from the update manifest. */
    static final class Metadata {
        final int versionCode;
        final String versionName;
        final int minSdk;
        final long sizeBytes;
        final String sha256;
        final String downloadPath;

        Metadata(int versionCode, String versionName, int minSdk, long sizeBytes,
                 String sha256, String downloadPath) {
            this.versionCode = versionCode;
            this.versionName = versionName;
            this.minSdk = minSdk;
            this.sizeBytes = sizeBytes;
            this.sha256 = sha256;
            this.downloadPath = downloadPath;
        }
    }

    private static final String METADATA_PATH = "/api/app-update/";
    private static final String DOWNLOAD_PATH_PREFIX = "/api/app-update/download/";

    private static final long MAX_METADATA_BYTES = 16L * 1024L;
    private static final long MAX_APK_BYTES = 512L * 1024L * 1024L;

    private static final int METADATA_CONNECT_TIMEOUT_MS = 10_000;
    private static final int METADATA_READ_TIMEOUT_MS = 15_000;
    private static final int DOWNLOAD_CONNECT_TIMEOUT_MS = 15_000;
    private static final int DOWNLOAD_READ_TIMEOUT_MS = 30_000;

    private static final int MAX_VERSION_CODE = 2_100_000_000;
    private static final Pattern SHA256_PATTERN = Pattern.compile("^[0-9a-f]{64}$");

    private static final String TLS_MESSAGE =
            "安全连接失败：无法验证服务器证书，请确认服务器证书有效且设备时间正确。";

    private final Context appContext;
    private final Listener listener;
    private final Handler mainHandler = new Handler(Looper.getMainLooper());
    private final Object lock = new Object();

    private int generation;
    private HttpURLConnection activeConnection;
    private Thread activeThread;
    private volatile boolean released;
    private File readyFile;
    private Metadata readyMetadata;

    AppUpdater(Context appContext, Listener listener) {
        this.appContext = appContext.getApplicationContext();
        this.listener = listener;
    }

    /** Asks the saved origin for its published update metadata. */
    void check(final String origin) {
        final int gen = beginTask();
        Thread thread = new Thread(() -> {
            Metadata metadata = null;
            String error = null;
            try {
                metadata = fetchMetadata(origin, gen);
            } catch (UpdateException e) {
                error = e.getMessage();
            } catch (RuntimeException e) {
                error = "检查更新失败，请稍后重试。";
            }
            if (error != null && !isCurrent(gen)) {
                return;
            }
            final Metadata result = metadata;
            final String failure = error;
            post(gen, () -> listener.onCheckFinished(result, failure));
        }, "app-update-check");
        launch(gen, thread);
    }

    /** Downloads, hashes and validates the published APK, then keeps it ready to install. */
    void download(final String origin, final Metadata metadata) {
        if (metadata == null) {
            return;
        }
        final int gen = beginTask();
        Thread thread = new Thread(() -> {
            File target = UpdateFileProvider.stagedFile(appContext);
            File directory = target.getParentFile();
            if (directory == null) {
                post(gen, () -> listener.onDownloadFinished(
                        null, metadata, "无法创建更新缓存目录。"));
                return;
            }
            File part = null;
            boolean success = false;
            String error = null;
            try {
                if (!directory.exists() && !directory.mkdirs()) {
                    throw new UpdateException("无法创建更新缓存目录。");
                }
                if (!directory.isDirectory()) {
                    throw new UpdateException("无法创建更新缓存目录。");
                }
                part = File.createTempFile("app-update-", ".part.apk", directory);
                HttpURLConnection connection = open(origin + metadata.downloadPath,
                        DOWNLOAD_CONNECT_TIMEOUT_MS, DOWNLOAD_READ_TIMEOUT_MS, gen);
                try {
                    int status = connection.getResponseCode();
                    if (status >= 300 && status < 400) {
                        throw new UpdateException("下载更新失败：服务器要求重定向，已拒绝。");
                    }
                    if (status == 404) {
                        throw new UpdateException("服务器上已没有此更新包，请重新检查更新。");
                    }
                    if (status != 200) {
                        throw new UpdateException("下载更新失败：服务器返回 HTTP " + status + "。");
                    }
                    long declared = connection.getContentLengthLong();
                    if (declared > MAX_APK_BYTES || declared > metadata.sizeBytes) {
                        throw new UpdateException("更新包大小与发布信息不一致，已停止下载。");
                    }
                    if (declared >= 0 && declared != metadata.sizeBytes) {
                        throw new UpdateException("更新包大小与发布信息不一致，已停止下载。");
                    }

                    MessageDigest digest = sha256();
                    long total = 0;
                    long lastProgress = 0;
                    try (InputStream in = connection.getInputStream();
                         OutputStream out = new FileOutputStream(part)) {
                        byte[] buffer = new byte[64 * 1024];
                        int read;
                        while ((read = in.read(buffer)) != -1) {
                            if (!isCurrent(gen)) {
                                return;
                            }
                            total += read;
                            if (total > metadata.sizeBytes || total > MAX_APK_BYTES) {
                                throw new UpdateException(
                                        "更新包大小与发布信息不一致，已停止下载。");
                            }
                            digest.update(buffer, 0, read);
                            out.write(buffer, 0, read);
                            long now = SystemClock.uptimeMillis();
                            if (now - lastProgress >= 150L) {
                                lastProgress = now;
                                final long downloaded = total;
                                post(gen, () -> listener.onDownloadProgress(
                                        downloaded, metadata.sizeBytes));
                            }
                        }
                    }
                    if (total != metadata.sizeBytes) {
                        throw new UpdateException("更新包大小与发布信息不一致，已停止下载。");
                    }
                    String actualHash = toHex(digest.digest());
                    if (!metadata.sha256.equals(actualHash)) {
                        throw new UpdateException(
                                "更新包校验失败（SHA-256 不匹配），已丢弃下载内容。");
                    }
                    if (!isCurrent(gen)) {
                        return;
                    }
                    // Full package/signature inspection happens before the file
                    // can ever be offered to the system installer.
                    validatePackage(part, metadata);
                    if (!isCurrent(gen)) {
                        return;
                    }
                    // Serialize the replace with cancellation: a cancelled
                    // generation must never overwrite a newer staged APK.
                    synchronized (lock) {
                        if (gen != generation || released) {
                            return;
                        }
                        if (target.exists() && !target.delete()) {
                            throw new UpdateException("无法替换旧的更新包。");
                        }
                        if (!part.renameTo(target)) {
                            throw new UpdateException("无法保存更新包。");
                        }
                        readyFile = target;
                        readyMetadata = metadata;
                    }
                    success = true;
                    post(gen, () -> listener.onDownloadFinished(target, metadata, null));
                } finally {
                    closeConnection(connection);
                }
            } catch (UpdateException e) {
                error = e.getMessage();
            } catch (SSLException e) {
                error = TLS_MESSAGE;
            } catch (SocketTimeoutException e) {
                error = "下载更新超时，请检查网络后重试。";
            } catch (UnknownHostException e) {
                error = "无法解析服务器地址，请检查网络连接。";
            } catch (ConnectException e) {
                error = "无法连接到服务器，请确认服务器已启动且网络可访问。";
            } catch (IOException e) {
                if (isCurrent(gen)) {
                    error = "下载更新失败，请检查网络后重试。";
                }
            } catch (RuntimeException e) {
                if (isCurrent(gen)) {
                    error = "下载更新失败，请稍后重试。";
                }
            } finally {
                if (!success) {
                    deleteFileQuietly(part);
                    if (error != null && isCurrent(gen)) {
                        final String failure = error;
                        post(gen, () -> listener.onDownloadFinished(null, metadata, failure));
                    }
                }
            }
        }, "app-update-download");
        launch(gen, thread);
    }

    /**
     * Re-hashes and re-inspects the already downloaded file, e.g. after the
     * user granted the unknown-sources permission and returned to the app.
     */
    void revalidateReady() {
        final int gen = beginTask();
        File file;
        Metadata metadata;
        synchronized (lock) {
            file = readyFile;
            metadata = readyMetadata;
        }
        final File candidate = file;
        final Metadata expected = metadata;
        Thread thread = new Thread(() -> {
            String error = null;
            if (candidate == null || expected == null) {
                error = "更新包已不存在，请重新检查更新。";
            } else {
                try {
                    verifyFile(candidate, expected);
                    if (!isCurrent(gen)) {
                        return;
                    }
                    validatePackage(candidate, expected);
                } catch (UpdateException e) {
                    error = e.getMessage();
                } catch (IOException e) {
                    error = "无法读取更新包，请重新下载。";
                } catch (RuntimeException e) {
                    error = "校验更新包失败，请重新下载。";
                }
            }
            if (error != null && !isCurrent(gen)) {
                return;
            }
            final String failure = error;
            post(gen, () -> listener.onRevalidateFinished(
                    failure == null ? candidate : null, failure));
        }, "app-update-verify");
        launch(gen, thread);
    }

    /** Cancels the current worker and drops its callbacks. */
    void cancel() {
        Thread thread;
        synchronized (lock) {
            generation++;
            closeActiveLocked();
            thread = activeThread;
            activeThread = null;
        }
        if (thread != null) {
            thread.interrupt();
        }
    }

    /** Cancels and permanently ignores all further results (Activity shutdown). */
    void release() {
        synchronized (lock) {
            released = true;
        }
        cancel();
        synchronized (lock) {
            activeThread = null;
        }
    }

    /** Deletes the staged APK and any partial downloads. */
    void discardReady() {
        File file;
        synchronized (lock) {
            file = readyFile;
            readyFile = null;
            readyMetadata = null;
        }
        deleteFileQuietly(file);
        File staged = UpdateFileProvider.stagedFile(appContext);
        // Also remove a target left behind by a previous process lifetime.
        deleteFileQuietly(staged);
        File directory = staged.getParentFile();
        File[] children = directory != null ? directory.listFiles() : null;
        if (children == null) {
            return;
        }
        for (File child : children) {
            String name = child.getName();
            if (name.startsWith("app-update-") && name.endsWith(".part.apk")) {
                deleteFileQuietly(child);
            }
        }
    }

    // ------------------------------------------------------------------
    // HTTP
    // ------------------------------------------------------------------

    private Metadata fetchMetadata(String origin, int gen) throws UpdateException {
        HttpURLConnection connection = open(origin + METADATA_PATH,
                METADATA_CONNECT_TIMEOUT_MS, METADATA_READ_TIMEOUT_MS, gen);
        try {
            int status = connection.getResponseCode();
            if (status >= 300 && status < 400) {
                throw new UpdateException("检查更新失败：服务器要求重定向，已拒绝。");
            }
            if (status == 404) {
                throw new UpdateException("服务器未提供更新检查（可能运行的是旧版本服务端）。");
            }
            if (status != 200) {
                throw new UpdateException("检查更新失败：服务器返回 HTTP " + status + "。");
            }
            long declared = connection.getContentLengthLong();
            if (declared > MAX_METADATA_BYTES) {
                throw new UpdateException("更新信息异常（响应过大）。");
            }
            try (InputStream in = connection.getInputStream()) {
                return parseMetadata(readBounded(in, MAX_METADATA_BYTES));
            }
        } catch (SocketTimeoutException e) {
            throw new UpdateException("检查更新超时，请检查网络后重试。");
        } catch (SSLException e) {
            throw new UpdateException(TLS_MESSAGE);
        } catch (UnknownHostException e) {
            throw new UpdateException("无法解析服务器地址，请检查网络连接。");
        } catch (ConnectException e) {
            throw new UpdateException("无法连接到服务器，请确认服务器已启动且网络可访问。");
        } catch (IOException e) {
            throw new UpdateException("检查更新失败，请检查网络后重试。");
        } finally {
            closeConnection(connection);
        }
    }

    private HttpURLConnection open(String address, int connectTimeout, int readTimeout, int gen)
            throws UpdateException {
        final URL url;
        try {
            url = new URL(address);
        } catch (MalformedURLException e) {
            throw new UpdateException("更新地址无效。");
        }
        if (!"http".equals(url.getProtocol()) && !"https".equals(url.getProtocol())) {
            throw new UpdateException("更新地址无效。");
        }
        final HttpURLConnection connection;
        try {
            connection = (HttpURLConnection) url.openConnection();
        } catch (IOException | ClassCastException e) {
            throw new UpdateException("无法建立更新连接。");
        }
        connection.setInstanceFollowRedirects(false);
        connection.setConnectTimeout(connectTimeout);
        connection.setReadTimeout(readTimeout);
        connection.setUseCaches(false);
        connection.setRequestProperty("Accept-Encoding", "identity");
        connection.setRequestProperty("Cache-Control", "no-store, no-cache");
        connection.setRequestProperty("Pragma", "no-cache");
        // No cookies or credentials are attached: WebView cookies live in
        // CookieManager, which HttpURLConnection never consults.
        synchronized (lock) {
            if (gen != generation || released) {
                connection.disconnect();
                throw new UpdateException("更新请求已取消。");
            }
            activeConnection = connection;
        }
        return connection;
    }

    private void closeConnection(HttpURLConnection connection) {
        if (connection == null) {
            return;
        }
        synchronized (lock) {
            if (activeConnection == connection) {
                activeConnection = null;
            }
        }
        connection.disconnect();
    }

    private void closeActiveLocked() {
        if (activeConnection != null) {
            activeConnection.disconnect();
            activeConnection = null;
        }
    }

    // ------------------------------------------------------------------
    // Metadata
    // ------------------------------------------------------------------

    private static Metadata parseMetadata(byte[] body) throws UpdateException {
        final JSONObject json;
        try {
            json = new JSONObject(new String(body, StandardCharsets.UTF_8));
        } catch (JSONException e) {
            throw new UpdateException("服务器返回的更新信息无效，已停止。");
        }
        Object available = json.opt("available");
        if (!(available instanceof Boolean)) {
            throw new UpdateException("服务器返回的更新信息无效，已停止。");
        }
        if (!((Boolean) available)) {
            return null;
        }
        int versionCode = (int) requiredIntegral(json, "version_code", 1, MAX_VERSION_CODE);
        String versionName = requiredString(json, "version_name", 64);
        int minSdk = (int) requiredIntegral(json, "min_sdk", 1, 10_000);
        long sizeBytes = requiredIntegral(json, "size_bytes", 1, MAX_APK_BYTES);
        String sha256 = requiredString(json, "sha256", 64);
        if (!SHA256_PATTERN.matcher(sha256).matches()) {
            throw new UpdateException("服务器返回的更新信息无效，已停止。");
        }
        String downloadPath = requiredString(json, "download_path", 128);
        if (!(DOWNLOAD_PATH_PREFIX + sha256 + "/").equals(downloadPath)) {
            throw new UpdateException("服务器返回的更新信息无效，已停止。");
        }
        return new Metadata(versionCode, versionName, minSdk, sizeBytes, sha256, downloadPath);
    }

    private static long requiredIntegral(JSONObject json, String key, long min, long max)
            throws UpdateException {
        Object value = json.opt(key);
        if (!(value instanceof Number)) {
            throw new UpdateException("服务器返回的更新信息无效，已停止。");
        }
        Number number = (Number) value;
        double asDouble = number.doubleValue();
        long asLong = number.longValue();
        if (Double.isNaN(asDouble) || Double.isInfinite(asDouble)
                || asDouble != (double) asLong || asLong < min || asLong > max) {
            throw new UpdateException("服务器返回的更新信息无效，已停止。");
        }
        return asLong;
    }

    private static String requiredString(JSONObject json, String key, int maxLength)
            throws UpdateException {
        Object value = json.opt(key);
        if (!(value instanceof String)) {
            throw new UpdateException("服务器返回的更新信息无效，已停止。");
        }
        String text = (String) value;
        if (text.isEmpty() || text.length() > maxLength) {
            throw new UpdateException("服务器返回的更新信息无效，已停止。");
        }
        for (int i = 0; i < text.length(); i++) {
            char c = text.charAt(i);
            if (c < 0x20 || c == 0x7f) {
                throw new UpdateException("服务器返回的更新信息无效，已停止。");
            }
        }
        return text;
    }

    private static byte[] readBounded(InputStream in, long limit)
            throws IOException, UpdateException {
        ByteArrayOutputStream out = new ByteArrayOutputStream();
        byte[] buffer = new byte[4096];
        long total = 0;
        int read;
        while ((read = in.read(buffer)) != -1) {
            total += read;
            if (total > limit) {
                throw new UpdateException("更新信息异常（响应过大）。");
            }
            out.write(buffer, 0, read);
        }
        return out.toByteArray();
    }

    // ------------------------------------------------------------------
    // APK inspection
    // ------------------------------------------------------------------

    private void verifyFile(File file, Metadata metadata) throws IOException, UpdateException {
        if (!file.isFile()) {
            throw new UpdateException("更新包已不存在，请重新检查更新。");
        }
        if (file.length() != metadata.sizeBytes) {
            throw new UpdateException("更新包大小校验失败，已拒绝安装。");
        }
        MessageDigest digest = sha256();
        try (InputStream in = new FileInputStream(file)) {
            byte[] buffer = new byte[64 * 1024];
            int read;
            while ((read = in.read(buffer)) != -1) {
                digest.update(buffer, 0, read);
            }
        }
        if (!metadata.sha256.equals(toHex(digest.digest()))) {
            throw new UpdateException("更新包完整性问题（SHA-256 不匹配），已拒绝安装。");
        }
    }

    private void validatePackage(File file, Metadata metadata) throws UpdateException {
        PackageManager packageManager = appContext.getPackageManager();
        String packageName = appContext.getPackageName();
        int flags = signatureFlags();
        final PackageInfo installed;
        final PackageInfo archive;
        try {
            installed = packageManager.getPackageInfo(packageName, flags);
        } catch (PackageManager.NameNotFoundException e) {
            throw new UpdateException("无法读取本机应用版本信息，已停止安装。");
        }
        archive = packageManager.getPackageArchiveInfo(file.getAbsolutePath(), flags);
        if (archive == null || archive.applicationInfo == null) {
            throw new UpdateException("更新包不是有效的 Android 安装包，已拒绝安装。");
        }
        if (!packageName.equals(archive.packageName)) {
            throw new UpdateException("更新包包名与本应用不一致，已拒绝安装。");
        }
        long installedCode = versionCodeOf(installed);
        long archiveCode = versionCodeOf(archive);
        if (archiveCode != metadata.versionCode) {
            throw new UpdateException("更新包版本与发布信息不一致，已拒绝安装。");
        }
        if (archiveCode <= installedCode) {
            throw new UpdateException("更新包版本不高于已安装版本，已拒绝安装。");
        }
        if (metadata.versionName != null
                && !metadata.versionName.equals(archive.versionName)) {
            throw new UpdateException("更新包版本名称与发布信息不一致，已拒绝安装。");
        }
        int archiveMinSdk = archive.applicationInfo.minSdkVersion;
        if (archiveMinSdk != metadata.minSdk) {
            throw new UpdateException("更新包最低系统版本与发布信息不一致，已拒绝安装。");
        }
        if (archiveMinSdk > Build.VERSION.SDK_INT) {
            throw new UpdateException("更新包与当前 Android 版本不兼容，已拒绝安装。");
        }
        Set<String> installedSigners = signerDigests(installed);
        Set<String> archiveSigners = signerDigests(archive);
        if (installedSigners == null || archiveSigners == null
                || installedSigners.isEmpty()
                || !installedSigners.equals(archiveSigners)) {
            throw new UpdateException("更新包签名与已安装应用不一致，已拒绝安装。");
        }
    }

    @SuppressWarnings("deprecation")
    private static int signatureFlags() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
            return PackageManager.GET_SIGNING_CERTIFICATES | PackageManager.GET_SIGNATURES;
        }
        return PackageManager.GET_SIGNATURES;
    }

    @SuppressWarnings("deprecation")
    private static Set<String> signerDigests(PackageInfo info) {
        Signature[] signatures = null;
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
            signatures = SignatureApi28.apkSigners(info);
        }
        if (signatures == null || signatures.length == 0) {
            signatures = info.signatures;
        }
        if (signatures == null || signatures.length == 0) {
            return null;
        }
        Set<String> digests = new TreeSet<>();
        for (Signature signature : signatures) {
            MessageDigest digest = sha256();
            digests.add(toHex(digest.digest(signature.toByteArray())));
        }
        return digests;
    }

    /** Kept in its own class so API 26/27 never resolve {@link android.content.pm.SigningInfo}. */
    @android.annotation.TargetApi(Build.VERSION_CODES.P)
    private static final class SignatureApi28 {
        static Signature[] apkSigners(PackageInfo info) {
            if (info.signingInfo == null) {
                return null;
            }
            return info.signingInfo.getApkContentsSigners();
        }
    }

    @SuppressWarnings("deprecation")
    private static long versionCodeOf(PackageInfo info) {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
            return info.getLongVersionCode();
        }
        return info.versionCode;
    }

    // ------------------------------------------------------------------
    // Utilities
    // ------------------------------------------------------------------

    private int beginTask() {
        synchronized (lock) {
            generation++;
            closeActiveLocked();
            return generation;
        }
    }

    private void launch(int gen, Thread thread) {
        synchronized (lock) {
            if (gen != generation || released) {
                return;
            }
            activeThread = thread;
        }
        thread.start();
    }

    private boolean isCurrent(int gen) {
        synchronized (lock) {
            return gen == generation && !released;
        }
    }

    private void post(int gen, Runnable action) {
        mainHandler.post(() -> {
            synchronized (lock) {
                if (gen != generation || released) {
                    return;
                }
            }
            action.run();
        });
    }

    private static MessageDigest sha256() {
        try {
            return MessageDigest.getInstance("SHA-256");
        } catch (NoSuchAlgorithmException e) {
            throw new IllegalStateException("SHA-256 unavailable", e);
        }
    }

    private static String toHex(byte[] bytes) {
        StringBuilder builder = new StringBuilder(bytes.length * 2);
        for (byte value : bytes) {
            builder.append(String.format(Locale.ROOT, "%02x", value & 0xff));
        }
        return builder.toString();
    }

    private static void deleteFileQuietly(File file) {
        if (file != null && file.exists()) {
            //noinspection ResultOfMethodCallIgnored
            file.delete();
        }
    }

    private static final class UpdateException extends Exception {
        UpdateException(String message) {
            super(message);
        }
    }
}
