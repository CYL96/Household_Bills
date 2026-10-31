package com.household.bills;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;

import org.junit.Test;

public class ServerAddressTest {

    @Test
    public void parseCanonicalizesLanHttpAddressWithExplicitPort() {
        ServerAddress.Result result = ServerAddress.parse("http://10.0.2.2:9890");

        assertTrue(result.isValid());
        assertEquals("http://10.0.2.2:9890", result.origin());
    }

    @Test
    public void parseNormalizesSchemesHostsPortsAndTrailingSlashes() {
        Object[][] cases = {
                {"HTTP://Example.COM:80/", "http://example.com"},
                {"HTTPS://Example.COM:443", "https://example.com"},
                {"http://example.com:8080/", "http://example.com:8080"},
                {"http://10.0.2.2:9890/", "http://10.0.2.2:9890"},
                {"http://[::1]:9890/", "http://[::1]:9890"},
                {"https://[2001:db8::1]/", "https://[2001:db8::1]"},
                {"  http://10.0.2.2:9890  ", "http://10.0.2.2:9890"},
                {"ledger.example.com", "https://ledger.example.com"},
                {"10.0.2.2:9890", "https://10.0.2.2:9890"},
                {"localhost:8080", "https://localhost:8080"},
        };
        for (Object[] c : cases) {
            String input = (String) c[0];
            String expected = (String) c[1];
            ServerAddress.Result result = ServerAddress.parse(input);
            assertTrue(input, result.isValid());
            assertEquals(input, expected, result.origin());
        }
    }

    @Test
    public void parseRejectsUnsafeOrNonRootAddresses() {
        String[] invalid = {
                null,
                "",
                "   ",
                "//example.com",
                "ftp://example.com",
                "mailto:ops@example.com",
                "javascript:alert(1)",
                "https://user:pass@example.com",
                "https://user@example.com",
                "http://example.com/?next=/bills",
                "http://example.com/#bills",
                "http://example.com/bills",
                "http://example.com:",
                "http://example.com:0",
                "http://example.com:65536",
                "http://example.com:port",
                "http://example.com:-1",
                "http://example.com\\bills",
                "http://exa mple.com",
                "http://10.0.2.2:9890 /",
        };
        for (String address : invalid) {
            assertFalse(String.valueOf(address), ServerAddress.parse(address).isValid());
        }
    }

    @Test
    public void originOfKeepsOriginForSpaRoutesButRejectsCredentials() {
        assertEquals("https://app.example.com",
                ServerAddress.originOf("HTTPS://App.Example.COM/#/bills?month=2026-10"));
        assertEquals("http://10.0.2.2:9890",
                ServerAddress.originOf("http://10.0.2.2:9890/app?tab=1#top"));
        assertEquals("https://example.com",
                ServerAddress.originOf("https://example.com:443/anything"));
        assertEquals("https://[::1]",
                ServerAddress.originOf("https://[::1]:443/x"));

        String[] rejected = {
                null,
                "",
                "not a url",
                "ftp://example.com",
                "file:///sdcard/index.html",
                "https://user:pass@app.example.com",
                "http://user@app.example.com",
                "http://example.com:99999",
                "http://exa mple.com",
                "http://example.com\\app",
                "//example.com",
                "javascript:alert(1)",
        };
        for (String url : rejected) {
            assertNull(String.valueOf(url), ServerAddress.originOf(url));
        }
    }
}
