package com.household.bills;

import java.net.URI;
import java.net.URISyntaxException;
import java.util.Arrays;
import java.util.HashSet;
import java.util.Locale;
import java.util.Set;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

/**
 * Validates the ledger server address and canonicalizes it to an origin.
 *
 * <p>Only HTTP(S) origins are accepted: {@code http://host[:port]} or
 * {@code https://host[:port]}, optionally followed by a single trailing
 * slash. A missing scheme defaults to HTTPS. User info, queries, fragments,
 * and non-root paths are rejected because the web application is served from
 * the root of its origin.</p>
 *
 * <p>This class uses only {@code java.*} APIs so it can be exercised without
 * an Android runtime.</p>
 */
final class ServerAddress {

    private static final Pattern SCHEME_PREFIX =
            Pattern.compile("^([A-Za-z][A-Za-z0-9+.-]*):(.*)$", Pattern.DOTALL);

    /** Schemes that must never be silently reinterpreted as HTTPS host names. */
    private static final Set<String> OTHER_SCHEMES = new HashSet<>(Arrays.asList(
            "about", "blob", "chrome", "content", "data", "file", "ftp", "ftps",
            "intent", "javascript", "mailto", "sms", "tel", "ws", "wss"));

    private ServerAddress() {
    }

    /** Outcome of {@link #parse(String)}. */
    static final class Result {
        final boolean valid;
        final String origin;
        final boolean schemeAdded;
        final String error;

        private Result(boolean valid, String origin, boolean schemeAdded, String error) {
            this.valid = valid;
            this.origin = origin;
            this.schemeAdded = schemeAdded;
            this.error = error;
        }

        boolean isValid() {
            return valid;
        }

        String origin() {
            return origin;
        }

        boolean schemeAdded() {
            return schemeAdded;
        }

        String error() {
            return error;
        }
    }

    /**
     * Parses user input into the canonical origin of the ledger server.
     * Surrounding whitespace is trimmed; whitespace inside the address is invalid.
     */
    static Result parse(String input) {
        if (input == null) {
            return fail("请输入服务器地址");
        }
        String s = input.trim();
        if (s.isEmpty()) {
            return fail("请输入服务器地址");
        }
        for (int i = 0; i < s.length(); i++) {
            char c = s.charAt(i);
            if (Character.isWhitespace(c) || Character.isSpaceChar(c) || c < 0x20 || c == 0x7F) {
                return fail("服务器地址不能包含空格、换行或控制字符");
            }
        }
        if (s.indexOf('\\') >= 0) {
            return fail("服务器地址格式不正确");
        }

        if (s.startsWith("//")) {
            return fail("请填写完整地址，例如 https://ledger.example.com");
        }

        boolean schemeAdded = false;
        Matcher matcher = SCHEME_PREFIX.matcher(s);
        if (matcher.matches()) {
            String candidate = matcher.group(1);
            String rest = matcher.group(2);
            String lower = candidate.toLowerCase(Locale.ROOT);
            if ("http".equals(lower) || "https".equals(lower)) {
                if (!rest.startsWith("//")) {
                    s = lower + "://" + stripLeadingSlashes(rest);
                } else {
                    s = lower + ":" + rest;
                }
            } else if (OTHER_SCHEMES.contains(lower)) {
                return fail("仅支持 http:// 或 https:// 服务器地址");
            } else if (candidate.indexOf('.') >= 0 || rest.isEmpty() || startsWithDigit(rest)) {
                // Looks like "host:port" rather than a scheme, for example
                // "localhost:8080" or "ledger.example.com:9890".
                s = "https://" + s;
                schemeAdded = true;
            } else {
                return fail("仅支持 http:// 或 https:// 服务器地址");
            }
        } else {
            s = "https://" + s;
            schemeAdded = true;
        }

        final URI uri;
        try {
            uri = new URI(s);
        } catch (URISyntaxException e) {
            return fail("服务器地址格式不正确");
        }

        if (uri.isOpaque() || uri.getScheme() == null) {
            return fail("服务器地址格式不正确");
        }
        String scheme = uri.getScheme().toLowerCase(Locale.ROOT);
        if (!"http".equals(scheme) && !"https".equals(scheme)) {
            return fail("仅支持 http:// 或 https:// 服务器地址");
        }
        if (uri.getRawUserInfo() != null) {
            return fail("服务器地址不能包含用户名或密码");
        }
        if (uri.getRawQuery() != null) {
            return fail("服务器地址不能包含查询参数（?）");
        }
        if (uri.getRawFragment() != null) {
            return fail("服务器地址不能包含锚点（#）");
        }
        String path = uri.getRawPath();
        if (path != null && !path.isEmpty() && !"/".equals(path)) {
            return fail("请填写服务器根地址，不要包含页面路径");
        }
        String authority = uri.getRawAuthority();
        String authorityError = authorityError(authority);
        if (authorityError != null) {
            return fail(authorityError);
        }
        String host = uri.getHost();
        if (host == null || host.isEmpty()) {
            return fail("无法识别服务器域名或 IP，请检查地址");
        }
        int port = authorityPort(authority);

        String origin = canonicalOrigin(scheme, host, port);
        if (origin == null) {
            return fail("服务器地址格式不正确");
        }
        return new Result(true, origin, schemeAdded, null);
    }

    /**
     * Returns the canonical origin of an absolute HTTP(S) URL, or {@code null}
     * when the URL is not a usable HTTP(S) origin. The path, query, and
     * fragment are ignored.
     */
    static String originOf(String url) {
        if (url == null) {
            return null;
        }
        String s = url.trim();
        if (s.isEmpty() || s.indexOf('\\') >= 0) {
            return null;
        }
        final URI uri;
        try {
            uri = new URI(s);
        } catch (URISyntaxException e) {
            return null;
        }
        if (uri.isOpaque() || uri.getScheme() == null || uri.getRawUserInfo() != null) {
            return null;
        }
        String scheme = uri.getScheme().toLowerCase(Locale.ROOT);
        if (!"http".equals(scheme) && !"https".equals(scheme)) {
            return null;
        }
        String authority = uri.getRawAuthority();
        if (authorityError(authority) != null) {
            return null;
        }
        String host = uri.getHost();
        if (host == null || host.isEmpty()) {
            return null;
        }
        return canonicalOrigin(scheme, host, authorityPort(authority));
    }

    /** Returns the lowercase URI scheme of {@code url}, or {@code null}. */
    static String schemeOf(String url) {
        if (url == null) {
            return null;
        }
        try {
            URI uri = new URI(url.trim());
            return uri.getScheme() == null ? null : uri.getScheme().toLowerCase(Locale.ROOT);
        } catch (URISyntaxException e) {
            return null;
        }
    }

    /** Convenience helper: canonical origin, or {@code null} when invalid. */
    static String normalize(String input) {
        Result result = parse(input);
        return result.isValid() ? result.origin : null;
    }

    /** Returns a Chinese error for an unusable authority, or {@code null}. */
    private static String authorityError(String authority) {
        if (authority == null || authority.isEmpty()) {
            return "无法识别服务器域名或 IP，请检查地址";
        }
        if (authority.indexOf('@') >= 0) {
            return "服务器地址不能包含用户名或密码";
        }
        String hostPart;
        String portPart = null;
        if (authority.charAt(0) == '[') {
            int close = authority.indexOf(']');
            if (close <= 1) {
                return "无法识别服务器域名或 IP，请检查地址";
            }
            hostPart = authority.substring(0, close + 1);
            String rest = authority.substring(close + 1);
            if (!rest.isEmpty()) {
                if (rest.charAt(0) != ':') {
                    return "无法识别服务器域名或 IP，请检查地址";
                }
                portPart = rest.substring(1);
            }
        } else {
            int colon = authority.indexOf(':');
            if (colon >= 0) {
                if (authority.indexOf(':', colon + 1) >= 0) {
                    return "无法识别服务器域名或 IP，请检查地址";
                }
                hostPart = authority.substring(0, colon);
                portPart = authority.substring(colon + 1);
            } else {
                hostPart = authority;
            }
            if (hostPart.isEmpty()
                    || hostPart.indexOf('[') >= 0
                    || hostPart.indexOf(']') >= 0) {
                return "无法识别服务器域名或 IP，请检查地址";
            }
        }
        if (hostPart.isEmpty()) {
            return "无法识别服务器域名或 IP，请检查地址";
        }
        if (portPart != null) {
            if (portPart.isEmpty()) {
                return "端口号无效（应为 1-65535）";
            }
            for (int i = 0; i < portPart.length(); i++) {
                char c = portPart.charAt(i);
                if (c < '0' || c > '9') {
                    return "端口号无效（应为 1-65535）";
                }
            }
            long value;
            try {
                value = Long.parseLong(portPart);
            } catch (NumberFormatException e) {
                return "端口号无效（应为 1-65535）";
            }
            if (value < 1 || value > 65535) {
                return "端口号无效（应为 1-65535）";
            }
        }
        return null;
    }

    /** Port from an authority already accepted by {@link #authorityError}. */
    private static int authorityPort(String authority) {
        if (authority == null || authority.isEmpty()) {
            return -1;
        }
        String rest;
        if (authority.charAt(0) == '[') {
            int close = authority.indexOf(']');
            if (close < 0) {
                return -1;
            }
            rest = authority.substring(close + 1);
        } else {
            int colon = authority.indexOf(':');
            if (colon < 0) {
                return -1;
            }
            rest = authority.substring(colon);
        }
        if (!rest.startsWith(":")) {
            return -1;
        }
        try {
            return Integer.parseInt(rest.substring(1));
        } catch (NumberFormatException e) {
            return -1;
        }
    }

    private static String canonicalOrigin(String scheme, String host, int port) {
        String normalizedHost = host.toLowerCase(Locale.ROOT);
        if (normalizedHost.isEmpty()) {
            return null;
        }
        boolean defaultPort = ("http".equals(scheme) && port == 80)
                || ("https".equals(scheme) && port == 443);
        String hostPart;
        if (normalizedHost.startsWith("[") && normalizedHost.endsWith("]")) {
            hostPart = normalizedHost;
        } else if (normalizedHost.indexOf(':') >= 0) {
            hostPart = "[" + normalizedHost + "]";
        } else {
            hostPart = normalizedHost;
        }
        if (port == -1 || defaultPort) {
            return scheme + "://" + hostPart;
        }
        return scheme + "://" + hostPart + ":" + port;
    }

    private static String stripLeadingSlashes(String value) {
        int i = 0;
        while (i < value.length() && value.charAt(i) == '/') {
            i++;
        }
        return value.substring(i);
    }

    private static boolean startsWithDigit(String value) {
        return !value.isEmpty() && value.charAt(0) >= '0' && value.charAt(0) <= '9';
    }

    private static Result fail(String message) {
        return new Result(false, null, false, message);
    }
}
