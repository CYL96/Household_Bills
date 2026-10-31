package com.household.bills;

import android.app.Activity;
import android.app.AlertDialog;
import android.content.ActivityNotFoundException;
import android.content.Intent;
import android.content.SharedPreferences;
import android.content.pm.ApplicationInfo;
import android.content.pm.PackageInfo;
import android.content.pm.PackageManager;
import android.content.res.ColorStateList;
import android.graphics.Bitmap;
import android.graphics.Color;
import android.graphics.Insets;
import android.graphics.Typeface;
import android.graphics.drawable.Drawable;
import android.graphics.drawable.GradientDrawable;
import android.graphics.drawable.RippleDrawable;
import android.net.Uri;
import android.net.http.SslError;
import android.os.Build;
import android.os.Bundle;
import android.provider.Settings;
import android.text.InputType;
import android.util.TypedValue;
import android.view.Gravity;
import android.view.View;
import android.view.ViewGroup;
import android.view.Window;
import android.view.WindowInsets;
import android.view.WindowInsetsController;
import android.view.WindowManager;
import android.view.inputmethod.EditorInfo;
import android.view.inputmethod.InputMethodManager;
import android.webkit.CookieManager;
import android.webkit.JsResult;
import android.webkit.SslErrorHandler;
import android.webkit.WebBackForwardList;
import android.webkit.WebChromeClient;
import android.webkit.WebHistoryItem;
import android.webkit.WebResourceError;
import android.webkit.WebResourceRequest;
import android.webkit.WebResourceResponse;
import android.webkit.WebSettings;
import android.webkit.WebStorage;
import android.webkit.WebView;
import android.webkit.WebViewClient;
import android.widget.Button;
import android.widget.EditText;
import android.widget.FrameLayout;
import android.widget.LinearLayout;
import android.widget.ProgressBar;
import android.widget.ScrollView;
import android.widget.TextView;
import android.widget.Toast;
import android.window.OnBackInvokedCallback;
import android.window.OnBackInvokedDispatcher;

import java.io.File;
import java.util.HashMap;
import java.util.Locale;
import java.util.Map;

/**
 * Single-activity shell for the household ledger web application.
 *
 * <p>In the normal web state the page owns the whole content area: there is
 * no native top bar. The page renders its own header and, when it needs the
 * native server-address screen, uses an in-page link to
 * {@code household-bills://settings}. The native shell accepts that link only
 * for the current page after a real user gesture, then shows the settings
 * screen with a native 48dp toolbar and a back action. The error screen keeps
 * its own retry/settings buttons and is shown without any native top bar.</p>
 *
 * <p>Security boundaries: only a single configured HTTP(S) origin is loaded;
 * cross-origin HTTP(S) navigations go to an external browser; other schemes
 * are rejected; file/content access, third-party cookies, mixed content, JS
 * bridges, and SSL error bypasses are disabled.</p>
 */
public class MainActivity extends Activity {

    private static final String PREFS_NAME = "household_bills";
    private static final String PREF_SERVER_ORIGIN = "server_origin";
    private static final String PREF_PENDING_CLEANUP = "pending_origin_cleanup";

    /**
     * Exact user-agent token the web page checks for (as a whitespace-separated
     * token) to decide whether to render its native-settings entry point.
     */
    private static final String APP_WEBVIEW_UA_TOKEN = "HouseholdBillsApp/1";
    /**
     * In-page link target that asks this activity to open the native settings
     * screen. It is intercepted, never loaded and never registered as a deep link.
     */
    private static final String SETTINGS_URI = "household-bills://settings";

    private static final String KEY_SAVED_ORIGIN = "saved_origin";
    private static final String KEY_VIEW_ORIGIN = "saved_view_origin";
    private static final String KEY_UI_STATE = "saved_ui_state";
    private static final String KEY_RETURN_STATE = "saved_return_state";
    private static final String KEY_DRAFT = "saved_draft";
    private static final String KEY_ERROR_TITLE = "saved_error_title";
    private static final String KEY_ERROR_DETAIL = "saved_error_detail";

    private static final int STATE_WEB = 0;
    private static final int STATE_SETTINGS = 1;
    private static final int STATE_ERROR = 2;

    private static final int UPDATE_IDLE = 0;
    private static final int UPDATE_CHECKING = 1;
    private static final int UPDATE_UP_TO_DATE = 2;
    private static final int UPDATE_AVAILABLE = 3;
    private static final int UPDATE_DOWNLOADING = 4;
    private static final int UPDATE_VALIDATING = 5;
    private static final int UPDATE_READY = 6;
    private static final int UPDATE_PERMISSION = 7;
    private static final int UPDATE_ERROR = 8;

    private static final int MATCH = ViewGroup.LayoutParams.MATCH_PARENT;
    private static final int WRAP = ViewGroup.LayoutParams.WRAP_CONTENT;

    private static final int COLOR_TEAL = Color.rgb(0, 137, 123);
    private static final int COLOR_TEAL_DARK = Color.rgb(0, 105, 92);
    private static final int COLOR_TEAL_PALE = Color.rgb(204, 235, 231);
    private static final int COLOR_SURFACE = Color.WHITE;
    private static final int COLOR_APP_BG = Color.rgb(247, 248, 250);
    private static final int COLOR_TEXT = Color.rgb(31, 41, 51);
    private static final int COLOR_TEXT_SECONDARY = Color.rgb(95, 107, 122);
    private static final int COLOR_DIVIDER = Color.rgb(226, 231, 235);
    private static final int COLOR_ERROR = Color.rgb(179, 38, 30);
    private static final int COLOR_WARNING = Color.rgb(138, 83, 0);
    private static final int COLOR_WARNING_BG = Color.rgb(255, 244, 229);

    private SharedPreferences prefs;
    private String committedOrigin;

    private FrameLayout root;
    private FrameLayout toolbarWrap;
    private FrameLayout contentFrame;
    private WebView webView;
    private ProgressBar progressBar;

    private ScrollView settingsView;
    private EditText addressInput;
    private TextView addressError;
    private TextView settingsStatus;
    private Button saveButton;
    private Button cancelButton;
    private LinearLayout cacheRefreshSection;
    private Button cacheRefreshButton;

    private LinearLayout updateSection;
    private TextView updateVersionView;
    private TextView updateLatestView;
    private TextView updateStatusView;
    private ProgressBar updateProgress;
    private Button updateCheckButton;
    private Button updateCancelButton;
    private AlertDialog updateDialog;

    private AppUpdater updater;
    private AppUpdater.Metadata updateMetadata;
    private File updateReadyFile;
    private int updatePhase = UPDATE_IDLE;
    private String updateMessage = "尚未检查更新。";
    private long updateDownloaded;
    private long updateTotalBytes;
    private boolean awaitingInstallPermission;
    private long installedVersionCodeCache = -1;
    private String installedVersionNameCache;

    private ScrollView errorView;
    private TextView errorTitleView;
    private TextView errorDetailView;

    private int state = STATE_WEB;
    private int settingsReturnState = STATE_WEB;
    private boolean switchingOrigin;
    private int switchGeneration;
    private int webViewGeneration;
    private boolean mainFrameLoading;
    /** URL of the main-frame navigation whose callbacks we currently trust. */
    private String currentMainFrameUrl;

    private String errorTitleText;
    private String errorDetailText;

    private AlertDialog activeJsDialog;
    private JsResult activeJsResult;
    private AlertDialog originConfirmDialog;

    private Object backCallback;

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        if (getActionBar() != null) {
            getActionBar().hide();
        }
        getWindow().setSoftInputMode(WindowManager.LayoutParams.SOFT_INPUT_ADJUST_RESIZE);

        boolean debuggable =
                (getApplicationInfo().flags & ApplicationInfo.FLAG_DEBUGGABLE) != 0;
        WebView.setWebContentsDebuggingEnabled(debuggable);

        prefs = getSharedPreferences(PREFS_NAME, MODE_PRIVATE);
        ServerAddress.Result stored =
                ServerAddress.parse(prefs.getString(PREF_SERVER_ORIGIN, ""));
        committedOrigin = stored.isValid() ? stored.origin : null;

        boolean pendingCleanup = prefs.getBoolean(PREF_PENDING_CLEANUP, false);
        if (committedOrigin == null && pendingCleanup) {
            prefs.edit().putBoolean(PREF_PENDING_CLEANUP, false).apply();
            pendingCleanup = false;
        }

        buildUi();
        updater = new AppUpdater(getApplicationContext(), new UpdateListener());
        configureWebView();
        applyWindowChrome();

        if (pendingCleanup) {
            startPendingCleanup();
            registerBackCallback();
            return;
        }

        String savedOrigin = null;
        String savedViewOrigin = null;
        int savedUiState = -1;
        int savedReturnState = STATE_WEB;
        String savedDraft = null;
        String savedErrorTitle = null;
        String savedErrorDetail = null;
        if (savedInstanceState != null) {
            savedOrigin = savedInstanceState.getString(KEY_SAVED_ORIGIN);
            savedViewOrigin = savedInstanceState.getString(KEY_VIEW_ORIGIN);
            savedUiState = savedInstanceState.getInt(KEY_UI_STATE, -1);
            savedReturnState = savedInstanceState.getInt(KEY_RETURN_STATE, STATE_WEB);
            savedDraft = savedInstanceState.getString(KEY_DRAFT);
            savedErrorTitle = savedInstanceState.getString(KEY_ERROR_TITLE);
            savedErrorDetail = savedInstanceState.getString(KEY_ERROR_DETAIL);
        }

        boolean sameSavedOrigin =
                committedOrigin != null && committedOrigin.equals(savedOrigin);
        boolean canRestoreWeb = savedInstanceState != null
                && sameSavedOrigin
                && savedViewOrigin != null
                && savedViewOrigin.equals(committedOrigin);
        boolean restoredPages = false;
        if (canRestoreWeb) {
            WebBackForwardList list = webView.restoreState(savedInstanceState);
            if (list != null) {
                WebHistoryItem item = list.getCurrentItem();
                String restoredUrl = item != null ? item.getUrl() : null;
                restoredPages = restoredUrl != null
                        && !"about:blank".equals(restoredUrl)
                        && committedOrigin.equals(ServerAddress.originOf(restoredUrl));
                if (restoredPages) {
                    currentMainFrameUrl = restoredUrl;
                }
            }
        }

        boolean restoreUi = savedInstanceState != null
                && (committedOrigin == null || sameSavedOrigin);

        if (committedOrigin == null) {
            enterSettings(STATE_WEB, true);
            if (savedDraft != null) {
                addressInput.setText(savedDraft);
                addressInput.setSelection(addressInput.getText().length());
            }
        } else if (restoreUi && savedUiState == STATE_SETTINGS) {
            enterSettings(savedReturnState == STATE_ERROR ? STATE_ERROR : STATE_WEB, true);
            if (savedDraft != null) {
                addressInput.setText(savedDraft);
                addressInput.setSelection(addressInput.getText().length());
            }
            if (savedReturnState == STATE_ERROR) {
                // The settings screen must return to the original error page
                // with its details intact after a configuration change.
                applyErrorContent(savedErrorTitle, savedErrorDetail);
            }
            if (!restoredPages) {
                loadOrigin();
            }
        } else if (restoreUi && savedUiState == STATE_ERROR) {
            showError(
                    savedErrorTitle != null ? savedErrorTitle : "无法打开账本",
                    savedErrorDetail != null
                            ? savedErrorDetail
                            : "上次页面加载失败，请重试或检查服务器地址。");
        } else {
            state = STATE_WEB;
            updateChrome();
            if (!restoredPages) {
                loadOrigin();
            }
        }

        registerBackCallback();
    }

    // ------------------------------------------------------------------
    // UI construction
    // ------------------------------------------------------------------

    private void buildUi() {
        root = new FrameLayout(this);
        root.setBackgroundColor(COLOR_SURFACE);

        LinearLayout column = new LinearLayout(this);
        column.setOrientation(LinearLayout.VERTICAL);
        column.setBackgroundColor(COLOR_SURFACE);

        // Only the settings screen uses this native 48dp toolbar. The web and
        // error states hide it so the page or error screen owns the full area.
        toolbarWrap = new FrameLayout(this);
        toolbarWrap.setBackgroundColor(COLOR_SURFACE);

        LinearLayout toolbar = new LinearLayout(this);
        toolbar.setOrientation(LinearLayout.HORIZONTAL);
        toolbar.setGravity(Gravity.CENTER_VERTICAL);
        toolbar.setPadding(dp(8), 0, dp(8), 0);

        TextView backAction = makeText("返回", 15, COLOR_TEAL_DARK, true);
        backAction.setGravity(Gravity.CENTER);
        backAction.setMinWidth(dp(56));
        backAction.setMinHeight(dp(48));
        backAction.setPadding(dp(12), 0, dp(12), 0);
        backAction.setContentDescription("返回");
        backAction.setBackground(selectableBorderless());
        backAction.setOnClickListener(v -> {
            if (!switchingOrigin) {
                cancelSettings();
            }
        });
        toolbar.addView(backAction,
                new LinearLayout.LayoutParams(WRAP, MATCH));

        TextView toolbarTitle = makeText("设置", 18, COLOR_TEXT, true);
        toolbarTitle.setSingleLine(true);
        toolbarTitle.setGravity(Gravity.CENTER_VERTICAL);
        toolbarTitle.setPadding(dp(8), 0, dp(8), 0);
        toolbar.addView(toolbarTitle,
                new LinearLayout.LayoutParams(0, MATCH, 1f));

        toolbarWrap.addView(toolbar, new FrameLayout.LayoutParams(MATCH, MATCH));

        View divider = new View(this);
        divider.setBackgroundColor(COLOR_DIVIDER);
        toolbarWrap.addView(divider,
                new FrameLayout.LayoutParams(MATCH, dp(1), Gravity.BOTTOM));

        column.addView(toolbarWrap, new LinearLayout.LayoutParams(MATCH, dp(48)));

        contentFrame = new FrameLayout(this);
        contentFrame.setBackgroundColor(COLOR_SURFACE);
        column.addView(contentFrame, new LinearLayout.LayoutParams(MATCH, 0, 1f));

        webView = new WebView(this);
        webView.setBackgroundColor(COLOR_SURFACE);
        contentFrame.addView(webView, new FrameLayout.LayoutParams(MATCH, MATCH));

        settingsView = buildSettingsView();
        settingsView.setVisibility(View.GONE);
        contentFrame.addView(settingsView, new FrameLayout.LayoutParams(MATCH, MATCH));

        errorView = buildErrorView();
        errorView.setVisibility(View.GONE);
        contentFrame.addView(errorView, new FrameLayout.LayoutParams(MATCH, MATCH));

        // A 2dp loading line pinned to the top of the content area: it never
        // takes a full row and is hidden while the native screens are shown.
        progressBar = new ProgressBar(this, null,
                android.R.attr.progressBarStyleHorizontal);
        progressBar.setIndeterminate(false);
        progressBar.setMax(100);
        progressBar.setProgress(0);
        progressBar.setMinimumHeight(0);
        progressBar.setPadding(0, 0, 0, 0);
        progressBar.setProgressTintList(ColorStateList.valueOf(COLOR_TEAL));
        progressBar.setProgressBackgroundTintList(ColorStateList.valueOf(COLOR_TEAL_PALE));
        progressBar.setVisibility(View.GONE);
        contentFrame.addView(progressBar,
                new FrameLayout.LayoutParams(MATCH, dp(2), Gravity.TOP));

        root.addView(column, new FrameLayout.LayoutParams(MATCH, MATCH));
        setContentView(root);
        updateChrome();
    }

    private ScrollView buildSettingsView() {
        ScrollView scroll = new ScrollView(this);
        scroll.setBackgroundColor(COLOR_SURFACE);
        scroll.setFillViewport(true);

        LinearLayout form = new LinearLayout(this);
        form.setOrientation(LinearLayout.VERTICAL);
        form.setPadding(dp(20), dp(22), dp(20), dp(28));
        scroll.addView(form, new ScrollView.LayoutParams(MATCH, WRAP));

        form.addView(makeText("服务器地址", 18, COLOR_TEXT, true));

        TextView intro = makeText(
                "填写家庭账本服务器的根地址，不要包含 /api 或页面路径。",
                13, COLOR_TEXT_SECONDARY, false);
        intro.setLineSpacing(dp(3), 1f);
        form.addView(intro, withTopMargin(6));

        addressInput = new EditText(this);
        addressInput.setSingleLine(true);
        addressInput.setTextSize(TypedValue.COMPLEX_UNIT_SP, 16);
        addressInput.setTextColor(COLOR_TEXT);
        addressInput.setHintTextColor(COLOR_TEXT_SECONDARY);
        addressInput.setHint("https://ledger.example.com");
        addressInput.setInputType(
                InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_VARIATION_URI);
        addressInput.setImeOptions(EditorInfo.IME_ACTION_DONE);
        addressInput.setSelectAllOnFocus(false);
        addressInput.setPadding(dp(14), dp(12), dp(14), dp(12));
        addressInput.setBackground(rounded(COLOR_SURFACE, 10, 1, COLOR_DIVIDER));
        addressInput.setOnEditorActionListener((v, actionId, event) -> {
            if (actionId == EditorInfo.IME_ACTION_DONE) {
                onSaveClicked();
                return true;
            }
            return false;
        });
        form.addView(addressInput, withTopMargin(14));

        TextView example = makeText(
                "可省略 https://，应用会自动补全为 HTTPS。例如：https://ledger.example.com",
                12, COLOR_TEXT_SECONDARY, false);
        example.setLineSpacing(dp(3), 1f);
        form.addView(example, withTopMargin(8));

        TextView warning = makeText(
                "安全提示：推荐使用 HTTPS。HTTP 为明文传输，登录凭据和账本数据可能被窃听或篡改，仅限可信内网使用。",
                12, COLOR_WARNING, false);
        warning.setLineSpacing(dp(3), 1f);
        warning.setPadding(dp(12), dp(10), dp(12), dp(10));
        warning.setBackground(rounded(COLOR_WARNING_BG, 8, 0, 0));
        form.addView(warning, withTopMargin(14));

        addressError = makeText("", 13, COLOR_ERROR, false);
        addressError.setVisibility(View.GONE);
        form.addView(addressError, withTopMargin(10));

        settingsStatus = makeText("", 13, COLOR_TEAL_DARK, false);
        settingsStatus.setVisibility(View.GONE);
        form.addView(settingsStatus, withTopMargin(10));

        saveButton = makeButton("保存并连接", true);
        saveButton.setOnClickListener(v -> onSaveClicked());
        form.addView(saveButton, withTopMargin(18));

        cancelButton = makeButton("取消", false);
        cancelButton.setOnClickListener(v -> {
            if (!switchingOrigin) {
                cancelSettings();
            }
        });
        cancelButton.setVisibility(View.GONE);
        form.addView(cancelButton, withTopMargin(10));

        cacheRefreshSection = new LinearLayout(this);
        cacheRefreshSection.setOrientation(LinearLayout.VERTICAL);
        cacheRefreshSection.setVisibility(View.GONE);

        TextView cacheTitle = makeText("页面缓存", 15, COLOR_TEXT, true);
        cacheRefreshSection.addView(cacheTitle);

        TextView cacheHint = makeText(
                "服务端更新后，重新获取最新页面资源。已登录状态会保留，"
                        + "上方输入框中尚未保存的地址不会生效。",
                12, COLOR_TEXT_SECONDARY, false);
        cacheHint.setLineSpacing(dp(3), 1f);
        cacheRefreshSection.addView(cacheHint, withTopMargin(6));

        cacheRefreshButton = makeButton("刷新页面缓存", false);
        cacheRefreshButton.setOnClickListener(v -> refreshPageCache());
        cacheRefreshSection.addView(cacheRefreshButton, withTopMargin(10));

        form.addView(cacheRefreshSection, withTopMargin(22));

        updateSection = buildUpdateSection();
        form.addView(updateSection, withTopMargin(22));

        return scroll;
    }

    private LinearLayout buildUpdateSection() {
        LinearLayout section = new LinearLayout(this);
        section.setOrientation(LinearLayout.VERTICAL);
        section.setVisibility(View.GONE);

        section.addView(makeText("应用更新", 15, COLOR_TEXT, true));

        TextView hint = makeText(
                "使用上方已保存的服务器检查新版本。下载后会在本机校验完整性、包名、"
                        + "版本与签名，再交给 Android 系统确认安装。",
                12, COLOR_TEXT_SECONDARY, false);
        hint.setLineSpacing(dp(3), 1f);
        section.addView(hint, withTopMargin(6));

        updateVersionView = makeText("", 13, COLOR_TEXT, false);
        section.addView(updateVersionView, withTopMargin(10));

        updateLatestView = makeText("服务器最新版本：尚未检查", 13,
                COLOR_TEXT_SECONDARY, false);
        section.addView(updateLatestView, withTopMargin(4));

        updateStatusView = makeText("", 13, COLOR_TEXT_SECONDARY, false);
        updateStatusView.setLineSpacing(dp(3), 1f);
        section.addView(updateStatusView, withTopMargin(6));

        updateProgress = new ProgressBar(this, null,
                android.R.attr.progressBarStyleHorizontal);
        updateProgress.setIndeterminate(false);
        updateProgress.setMax(100);
        updateProgress.setProgress(0);
        updateProgress.setProgressTintList(ColorStateList.valueOf(COLOR_TEAL));
        updateProgress.setProgressBackgroundTintList(
                ColorStateList.valueOf(COLOR_TEAL_PALE));
        updateProgress.setVisibility(View.GONE);
        section.addView(updateProgress, withTopMargin(8));

        updateCheckButton = makeButton("检查更新", false);
        updateCheckButton.setOnClickListener(v -> onUpdateAction());
        section.addView(updateCheckButton, withTopMargin(10));

        updateCancelButton = makeButton("取消", false);
        updateCancelButton.setOnClickListener(v -> cancelUpdateTask());
        updateCancelButton.setVisibility(View.GONE);
        section.addView(updateCancelButton, withTopMargin(10));

        return section;
    }

    private ScrollView buildErrorView() {
        ScrollView scroll = new ScrollView(this);
        scroll.setBackgroundColor(COLOR_SURFACE);
        scroll.setFillViewport(true);

        LinearLayout box = new LinearLayout(this);
        box.setOrientation(LinearLayout.VERTICAL);
        box.setGravity(Gravity.CENTER);
        box.setPadding(dp(28), dp(32), dp(28), dp(32));
        scroll.addView(box, new ScrollView.LayoutParams(MATCH, WRAP));

        errorTitleView = makeText("无法打开账本", 20, COLOR_TEXT, true);
        errorTitleView.setGravity(Gravity.CENTER);
        box.addView(errorTitleView, new LinearLayout.LayoutParams(MATCH, WRAP));

        errorDetailView = makeText("", 14, COLOR_TEXT_SECONDARY, false);
        errorDetailView.setGravity(Gravity.CENTER);
        errorDetailView.setLineSpacing(dp(4), 1f);
        box.addView(errorDetailView, withTopMargin(12));

        Button retryButton = makeButton("重试", true);
        retryButton.setOnClickListener(v -> retryLoad());
        box.addView(retryButton, withTopMargin(24));

        Button errorSettingsButton = makeButton("设置", false);
        errorSettingsButton.setOnClickListener(
                v -> enterSettings(STATE_ERROR, true));
        box.addView(errorSettingsButton, withTopMargin(10));

        return scroll;
    }

    private TextView makeText(String text, float sizeSp, int color, boolean bold) {
        TextView view = new TextView(this);
        view.setText(text);
        view.setTextSize(TypedValue.COMPLEX_UNIT_SP, sizeSp);
        view.setTextColor(color);
        if (bold) {
            view.setTypeface(Typeface.create(Typeface.DEFAULT, Typeface.BOLD));
        }
        return view;
    }

    private Button makeButton(String label, boolean primary) {
        Button button = new Button(this);
        button.setAllCaps(false);
        button.setText(label);
        button.setTextSize(TypedValue.COMPLEX_UNIT_SP, 16);
        button.setTextColor(primary ? COLOR_SURFACE : COLOR_TEAL_DARK);
        button.setTypeface(Typeface.create(
                Typeface.DEFAULT, primary ? Typeface.BOLD : Typeface.NORMAL));
        GradientDrawable background = primary
                ? rounded(COLOR_TEAL, 10, 0, 0)
                : rounded(COLOR_SURFACE, 10, 1, COLOR_TEAL);
        button.setBackground(new RippleDrawable(
                ColorStateList.valueOf(primary ? 0x33FFFFFF : 0x1F00897B),
                background, null));
        button.setStateListAnimator(null);
        button.setElevation(0);
        button.setMinHeight(dp(48));
        button.setMinimumHeight(dp(48));
        button.setMinWidth(0);
        button.setPadding(dp(16), dp(12), dp(16), dp(12));
        button.setGravity(Gravity.CENTER);
        return button;
    }

    private GradientDrawable rounded(int fill, int radiusDp, int strokeDp, int strokeColor) {
        GradientDrawable drawable = new GradientDrawable();
        drawable.setShape(GradientDrawable.RECTANGLE);
        drawable.setColor(fill);
        drawable.setCornerRadius(dp(radiusDp));
        if (strokeDp > 0) {
            drawable.setStroke(dp(strokeDp), strokeColor);
        }
        return drawable;
    }

    private Drawable selectableBorderless() {
        TypedValue value = new TypedValue();
        if (getTheme().resolveAttribute(
                android.R.attr.selectableItemBackgroundBorderless, value, true)
                && value.resourceId != 0) {
            return getDrawable(value.resourceId);
        }
        return null;
    }

    private LinearLayout.LayoutParams withTopMargin(int topDp) {
        LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(MATCH, WRAP);
        params.topMargin = dp(topDp);
        return params;
    }

    private int dp(int value) {
        return Math.round(value * getResources().getDisplayMetrics().density);
    }

    // ------------------------------------------------------------------
    // Window insets / system bars
    // ------------------------------------------------------------------

    private void applyWindowChrome() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
            applyEdgeToEdgeWindowChrome();
        } else {
            applyLegacyWindowChrome();
        }
    }

    @android.annotation.TargetApi(Build.VERSION_CODES.R)
    private void applyEdgeToEdgeWindowChrome() {
        Window window = getWindow();
        // Android 15 (target 35) enforces edge-to-edge; handle every inset
        // ourselves so the page is never covered by system bars or the IME.
        window.setDecorFitsSystemWindows(false);
        window.setStatusBarColor(Color.TRANSPARENT);
        window.setNavigationBarColor(Color.TRANSPARENT);
        window.setNavigationBarContrastEnforced(false);
        WindowInsetsController controller = window.getInsetsController();
        if (controller != null) {
            int appearance = WindowInsetsController.APPEARANCE_LIGHT_STATUS_BARS
                    | WindowInsetsController.APPEARANCE_LIGHT_NAVIGATION_BARS;
            controller.setSystemBarsAppearance(appearance, appearance);
        }
        root.setOnApplyWindowInsetsListener((view, insets) -> {
            Insets bars = insets.getInsets(
                    WindowInsets.Type.systemBars()
                            | WindowInsets.Type.displayCutout());
            Insets ime = insets.getInsets(WindowInsets.Type.ime());
            int bottom = Math.max(bars.bottom, ime.bottom);
            view.setPadding(bars.left, bars.top, bars.right, bottom);
            return insets;
        });
        root.requestApplyInsets();
    }

    private void applyLegacyWindowChrome() {
        Window window = getWindow();
        window.setStatusBarColor(COLOR_APP_BG);
        window.setNavigationBarColor(COLOR_APP_BG);
        window.getDecorView().setSystemUiVisibility(
                View.SYSTEM_UI_FLAG_LIGHT_STATUS_BAR
                        | View.SYSTEM_UI_FLAG_LIGHT_NAVIGATION_BAR);
    }

    // ------------------------------------------------------------------
    // WebView setup and navigation policy
    // ------------------------------------------------------------------

    private void configureWebView() {
        WebSettings settings = webView.getSettings();
        settings.setJavaScriptEnabled(true);
        settings.setDomStorageEnabled(true);
        settings.setDatabaseEnabled(false);
        settings.setAllowFileAccess(false);
        settings.setAllowContentAccess(false);
        settings.setAllowFileAccessFromFileURLs(false);
        settings.setAllowUniversalAccessFromFileURLs(false);
        settings.setJavaScriptCanOpenWindowsAutomatically(false);
        settings.setSupportMultipleWindows(false);
        settings.setMixedContentMode(WebSettings.MIXED_CONTENT_NEVER_ALLOW);
        settings.setMediaPlaybackRequiresUserGesture(true);
        settings.setGeolocationEnabled(false);
        settings.setSaveFormData(false);

        // Capability hint only: the page renders its settings entry point when
        // this exact whitespace-separated token is present. Nothing about the
        // native state is exposed through it, and the page cannot read or change
        // the configured origin.
        String userAgent = settings.getUserAgentString();
        if (userAgent == null) {
            userAgent = "";
        }
        if (!hasUserAgentToken(userAgent)) {
            settings.setUserAgentString(userAgent.isEmpty()
                    ? APP_WEBVIEW_UA_TOKEN
                    : userAgent + " " + APP_WEBVIEW_UA_TOKEN);
        }

        CookieManager cookieManager = CookieManager.getInstance();
        cookieManager.setAcceptCookie(!switchingOrigin);
        cookieManager.setAcceptThirdPartyCookies(webView, false);

        webView.setWebViewClient(new LedgerWebViewClient(webViewGeneration));
        webView.setWebChromeClient(new LedgerChromeClient(webViewGeneration));
        webView.setOverScrollMode(View.OVER_SCROLL_IF_CONTENT_SCROLLS);
    }

    /** True when the user agent already carries the token as a whole token. */
    private static boolean hasUserAgentToken(String userAgent) {
        if (userAgent == null) {
            return false;
        }
        for (String token : userAgent.trim().split("\\s+")) {
            if (APP_WEBVIEW_UA_TOKEN.equals(token)) {
                return true;
            }
        }
        return false;
    }

    private final class LedgerWebViewClient extends WebViewClient {

        private final int generation;

        LedgerWebViewClient(int generation) {
            this.generation = generation;
        }

        /** Old WebViews (origin switches) must never influence the new page. */
        private boolean isCurrent(WebView view) {
            return view == webView && generation == webViewGeneration;
        }

        @Override
        public boolean shouldOverrideUrlLoading(WebView view, WebResourceRequest request) {
            if (!isCurrent(view)) {
                return true;
            }
            if (request == null || request.getUrl() == null) {
                return true;
            }
            String url = request.getUrl().toString();
            if (SETTINGS_URI.equals(url)) {
                handleSettingsLink(view, request);
                return true;
            }
            return handleNavigation(url, request.isForMainFrame());
        }

        @Override
        @SuppressWarnings("deprecation")
        public boolean shouldOverrideUrlLoading(WebView view, String url) {
            if (!isCurrent(view)) {
                return true;
            }
            // This legacy callback carries neither frame nor gesture
            // information, so it must never open the native settings screen.
            // handleNavigation() blocks every custom scheme.
            return handleNavigation(url, true);
        }

        /**
         * Opens the native settings screen only for a real user click on the
         * exact settings URI from the current page. The URI must belong to the
         * main frame, the page must still be the committed origin, and no origin
         * switch may be in progress. Anything else is silently ignored.
         */
        private void handleSettingsLink(WebView view, WebResourceRequest request) {
            if (!request.isForMainFrame()
                    || !request.hasGesture()
                    || state != STATE_WEB
                    || switchingOrigin) {
                return;
            }
            if (!isCurrent(view) || committedOrigin == null) {
                return;
            }
            String pageOrigin = ServerAddress.originOf(view.getUrl());
            if (pageOrigin == null || !committedOrigin.equals(pageOrigin)) {
                return;
            }
            enterSettings(STATE_WEB, true);
        }

        @Override
        public void onPageStarted(WebView view, String url, Bitmap favicon) {
            if (!isCurrent(view)) {
                return;
            }
            if (url == null || "about:blank".equals(url)) {
                return;
            }
            if (switchingOrigin) {
                return;
            }
            if (committedOrigin == null) {
                view.stopLoading();
                return;
            }
            String origin = ServerAddress.originOf(url);
            if (origin == null || !committedOrigin.equals(origin)) {
                // Also covers server-side redirects, which may bypass
                // shouldOverrideUrlLoading on some WebView versions.
                view.stopLoading();
                showError(
                        "已阻止页面跳转",
                        "页面尝试访问已配置服务器之外的地址，为保护登录状态，已阻止加载。\n\n当前服务器："
                                + committedOrigin);
                return;
            }
            currentMainFrameUrl = url;
            mainFrameLoading = true;
            progressBar.setProgress(5);
            if (state == STATE_WEB) {
                progressBar.setVisibility(View.VISIBLE);
            }
        }

        @Override
        public void onPageFinished(WebView view, String url) {
            if (!isCurrent(view) || switchingOrigin) {
                return;
            }
            if ("about:blank".equals(url)) {
                return;
            }
            mainFrameLoading = false;
            progressBar.setProgress(100);
            progressBar.setVisibility(View.GONE);
        }

        @Override
        public void onReceivedError(WebView view, WebResourceRequest request,
                                    WebResourceError error) {
            if (!isCurrent(view)) {
                return;
            }
            if (request == null || !request.isForMainFrame()) {
                return;
            }
            if (switchingOrigin || !isCurrentMainFrameRequest(request)) {
                return;
            }
            int code = error != null ? error.getErrorCode() : WebViewClient.ERROR_UNKNOWN;
            if (code == WebViewClient.ERROR_FAILED_SSL_HANDSHAKE) {
                showTlsError();
                return;
            }
            showError("无法连接到服务器",
                    networkErrorText(code)
                            + "\n\n目标：" + displayOrigin(committedOrigin));
        }

        @Override
        public void onReceivedHttpError(WebView view, WebResourceRequest request,
                                        WebResourceResponse errorResponse) {
            if (!isCurrent(view)) {
                return;
            }
            if (request == null || !request.isForMainFrame()) {
                // API/subresource failures must not replace a working page.
                return;
            }
            if (switchingOrigin || !isCurrentMainFrameRequest(request)) {
                return;
            }
            int status = errorResponse != null ? errorResponse.getStatusCode() : 0;
            if (status < 400) {
                return;
            }
            String url = request.getUrl() != null ? request.getUrl().toString() : "";
            showError("服务器返回错误",
                    "服务器响应 HTTP " + status + "（" + httpReason(status)
                            + "）。\n\n目标：" + url);
        }

        @Override
        public void onReceivedSslError(WebView view, SslErrorHandler handler, SslError error) {
            // Never proceed: a certificate failure always stops the load.
            if (handler != null) {
                handler.cancel();
            }
            if (!isCurrent(view) || switchingOrigin) {
                return;
            }
            String failedUrl = error != null ? error.getUrl() : null;
            String failedOrigin = ServerAddress.originOf(failedUrl);
            if (failedUrl == null || failedOrigin == null) {
                if (mainFrameLoading) {
                    showTlsError();
                }
                return;
            }
            if (!failedOrigin.equals(committedOrigin)) {
                return;
            }
            if (currentMainFrameUrl != null
                    && urlsReferToSameNavigation(failedUrl, currentMainFrameUrl)) {
                showTlsError();
            } else if (mainFrameLoading && currentMainFrameUrl == null) {
                showTlsError();
            }
        }
    }

    /**
     * Accepts error callbacks only when they belong to the main-frame
     * navigation currently committed to this WebView.
     */
    private boolean isCurrentMainFrameRequest(WebResourceRequest request) {
        if (request == null || request.getUrl() == null || currentMainFrameUrl == null) {
            return false;
        }
        String url = request.getUrl().toString();
        String origin = ServerAddress.originOf(url);
        if (origin == null || committedOrigin == null || !committedOrigin.equals(origin)) {
            return false;
        }
        return urlsReferToSameNavigation(url, currentMainFrameUrl);
    }

    private boolean urlsReferToSameNavigation(String actual, String expected) {
        if (actual == null || expected == null) {
            return false;
        }
        String actualUrl = stripFragment(actual);
        String expectedUrl = stripFragment(expected);
        if (actualUrl.equals(expectedUrl)) {
            return true;
        }
        // WebView may normalize a bare origin to a trailing slash.
        return actualUrl.equals(expectedUrl + "/")
                || expectedUrl.equals(actualUrl + "/");
    }

    private String stripFragment(String url) {
        int index = url.indexOf('#');
        return index >= 0 ? url.substring(0, index) : url;
    }

    private boolean handleNavigation(String url, boolean mainFrame) {
        if (switchingOrigin || url == null) {
            return true;
        }
        if (url.startsWith("about:")) {
            return true;
        }
        String scheme = ServerAddress.schemeOf(url);
        if (!"http".equals(scheme) && !"https".equals(scheme)) {
            // file:, content:, intent:, javascript:, mailto:, data:, ...
            return true;
        }
        String origin = ServerAddress.originOf(url);
        if (committedOrigin == null || origin == null) {
            return true;
        }
        if (committedOrigin.equals(origin)) {
            if (mainFrame) {
                // HTTP failures can arrive before onPageStarted().
                currentMainFrameUrl = url;
                mainFrameLoading = true;
            }
            return false;
        }
        if (!mainFrame) {
            // Do not launch external apps for third-party subframe content.
            return true;
        }
        openExternalBrowser(url);
        return true;
    }

    private void openExternalBrowser(String url) {
        try {
            Intent intent = new Intent(Intent.ACTION_VIEW, Uri.parse(url));
            intent.addCategory(Intent.CATEGORY_BROWSABLE);
            startActivity(intent);
        } catch (ActivityNotFoundException | SecurityException e) {
            Toast.makeText(this, "未找到可打开该链接的应用", Toast.LENGTH_SHORT).show();
        }
    }

    private final class LedgerChromeClient extends WebChromeClient {

        private final int generation;

        LedgerChromeClient(int generation) {
            this.generation = generation;
        }

        private boolean isCurrent(WebView view) {
            return view == webView && generation == webViewGeneration;
        }

        @Override
        public void onProgressChanged(WebView view, int newProgress) {
            if (!isCurrent(view) || switchingOrigin) {
                return;
            }
            if (newProgress >= 100) {
                progressBar.setProgress(100);
                progressBar.setVisibility(View.GONE);
                return;
            }
            progressBar.setProgress(newProgress);
            if (state == STATE_WEB) {
                progressBar.setVisibility(View.VISIBLE);
            }
        }

        @Override
        public boolean onJsAlert(WebView view, String url, String message, JsResult result) {
            if (!isCurrent(view)) {
                if (result != null) {
                    result.cancel();
                }
                return true;
            }
            showJsDialog(null, message, "确定", null, result);
            return true;
        }

        @Override
        public boolean onJsConfirm(WebView view, String url, String message, JsResult result) {
            if (!isCurrent(view)) {
                if (result != null) {
                    result.cancel();
                }
                return true;
            }
            showJsDialog("确认", message, "确定", "取消", result);
            return true;
        }
    }

    private void showJsDialog(String title, String message, String positive,
                              String negative, JsResult result) {
        if (result == null) {
            return;
        }
        if (isFinishing() || isDestroyed()) {
            result.cancel();
            return;
        }
        if (activeJsDialog != null) {
            JsResult previous = activeJsResult;
            activeJsDialog.dismiss();
            activeJsDialog = null;
            activeJsResult = null;
            if (previous != null) {
                previous.cancel();
            }
        }
        activeJsResult = result;
        AlertDialog.Builder builder = new AlertDialog.Builder(this)
                .setMessage(message != null ? message : "")
                .setCancelable(true)
                .setPositiveButton(positive, (dialog, which) -> {
                    activeJsDialog = null;
                    activeJsResult = null;
                    result.confirm();
                })
                .setOnCancelListener(dialog -> {
                    activeJsDialog = null;
                    activeJsResult = null;
                    result.cancel();
                });
        if (title != null) {
            builder.setTitle(title);
        }
        if (negative != null) {
            builder.setNegativeButton(negative, (dialog, which) -> {
                activeJsDialog = null;
                activeJsResult = null;
                result.cancel();
            });
        }
        activeJsDialog = builder.create();
        activeJsDialog.show();
    }

    private void cancelActiveJsDialog() {
        if (activeJsDialog != null) {
            JsResult pending = activeJsResult;
            activeJsDialog.dismiss();
            activeJsDialog = null;
            activeJsResult = null;
            if (pending != null) {
                pending.cancel();
            }
        } else if (activeJsResult != null) {
            JsResult pending = activeJsResult;
            activeJsResult = null;
            pending.cancel();
        }
    }

    // ------------------------------------------------------------------
    // State and navigation
    // ------------------------------------------------------------------

    private void updateChrome() {
        if (toolbarWrap == null) {
            return;
        }
        boolean settingsVisible = state == STATE_SETTINGS;
        // The web page and the error screen never get the native toolbar.
        toolbarWrap.setVisibility(settingsVisible ? View.VISIBLE : View.GONE);
        if (webView != null) {
            webView.setVisibility(state == STATE_WEB ? View.VISIBLE : View.INVISIBLE);
        }
        if (settingsView != null) {
            settingsView.setVisibility(settingsVisible ? View.VISIBLE : View.GONE);
        }
        if (errorView != null) {
            errorView.setVisibility(state == STATE_ERROR ? View.VISIBLE : View.GONE);
        }
        if (progressBar != null) {
            if (!settingsVisible && state == STATE_WEB && mainFrameLoading) {
                progressBar.setVisibility(View.VISIBLE);
            } else if (state != STATE_WEB) {
                // Never draw the loading line over the native settings/error UI.
                progressBar.setVisibility(View.GONE);
            }
        }
        if (cacheRefreshSection != null) {
            boolean hasSavedOrigin = committedOrigin != null;
            cacheRefreshSection.setVisibility(hasSavedOrigin ? View.VISIBLE : View.GONE);
            if (cacheRefreshButton != null) {
                cacheRefreshButton.setEnabled(hasSavedOrigin && !switchingOrigin);
            }
        }
        refreshUpdateUi();
    }

    private void enterSettings(int returnState, boolean resetDraft) {
        if (switchingOrigin) {
            return;
        }
        hideKeyboard();
        settingsReturnState = returnState == STATE_ERROR ? STATE_ERROR : STATE_WEB;
        if (resetDraft) {
            addressInput.setText(committedOrigin != null ? committedOrigin : "");
            addressInput.setSelection(addressInput.getText().length());
        }
        addressError.setVisibility(View.GONE);
        settingsStatus.setVisibility(View.GONE);
        cancelButton.setVisibility(committedOrigin != null ? View.VISIBLE : View.GONE);
        saveButton.setEnabled(true);
        cancelButton.setEnabled(true);
        addressInput.setEnabled(true);
        state = STATE_SETTINGS;
        updateChrome();
    }

    private void cancelSettings() {
        if (switchingOrigin) {
            return;
        }
        clearUpdateState();
        if (committedOrigin == null) {
            finish();
            return;
        }
        hideKeyboard();
        addressError.setVisibility(View.GONE);
        settingsStatus.setVisibility(View.GONE);
        state = settingsReturnState == STATE_ERROR ? STATE_ERROR : STATE_WEB;
        updateChrome();
    }

    private void handleSameOrigin() {
        hideKeyboard();
        clearUpdateState();
        addressError.setVisibility(View.GONE);
        settingsStatus.setVisibility(View.GONE);
        boolean shouldRetry = settingsReturnState == STATE_ERROR;
        state = STATE_WEB;
        updateChrome();
        if (shouldRetry) {
            loadOrigin();
        }
    }

    private void onSaveClicked() {
        if (switchingOrigin) {
            return;
        }
        hideKeyboard();
        ServerAddress.Result result =
                ServerAddress.parse(addressInput.getText().toString());
        if (!result.isValid()) {
            addressError.setText(result.error != null
                    ? result.error
                    : "服务器地址格式不正确");
            addressError.setVisibility(View.VISIBLE);
            settingsStatus.setVisibility(View.GONE);
            return;
        }
        addressError.setVisibility(View.GONE);
        settingsStatus.setVisibility(View.GONE);

        if (committedOrigin != null && committedOrigin.equals(result.origin)) {
            // Same origin: keep cookies, storage and the current page.
            handleSameOrigin();
            return;
        }
        if (committedOrigin == null) {
            committedOrigin = result.origin;
            prefs.edit().putString(PREF_SERVER_ORIGIN, committedOrigin).apply();
            state = STATE_WEB;
            updateChrome();
            loadOrigin();
            return;
        }
        confirmOriginChange(committedOrigin, result.origin);
    }

    private void confirmOriginChange(String oldOrigin, String newOrigin) {
        if (originConfirmDialog != null) {
            return;
        }
        saveButton.setEnabled(false);
        final AlertDialog dialog = new AlertDialog.Builder(this)
                .setTitle("更换服务器地址")
                .setMessage("即将从\n" + oldOrigin + "\n切换到\n" + newOrigin
                        + "\n\n为避免把本机登录状态带到其他站点，将清除当前服务器的 Cookie、"
                        + "网页存储、缓存与浏览历史，并需要重新登录。")
                .setNegativeButton("取消", (d, which) -> saveButton.setEnabled(true))
                .setPositiveButton("清除并切换", (d, which) -> beginOriginSwitch(newOrigin))
                .create();
        dialog.setOnDismissListener(d -> {
            if (originConfirmDialog == dialog) {
                originConfirmDialog = null;
            }
            if (!switchingOrigin) {
                saveButton.setEnabled(true);
            }
        });
        originConfirmDialog = dialog;
        dialog.show();
    }

    private void beginOriginSwitch(String newOrigin) {
        if (switchingOrigin) {
            return;
        }
        switchingOrigin = true;
        final int generation = ++switchGeneration;
        clearUpdateState();
        committedOrigin = newOrigin;
        // Persist both values together: if the process dies mid-cleanup, the
        // next launch sees the pending flag and clears old data before loading.
        prefs.edit()
                .putString(PREF_SERVER_ORIGIN, newOrigin)
                .putBoolean(PREF_PENDING_CLEANUP, true)
                .apply();
        saveButton.setEnabled(false);
        cancelButton.setEnabled(false);
        addressInput.setEnabled(false);
        if (cacheRefreshButton != null) {
            cacheRefreshButton.setEnabled(false);
        }
        addressError.setVisibility(View.GONE);
        settingsStatus.setText("正在清理旧站点的登录状态与缓存…");
        settingsStatus.setVisibility(View.VISIBLE);
        progressBar.setVisibility(View.GONE);
        recreateWebViewForCleanup(generation);
    }

    private void startPendingCleanup() {
        if (committedOrigin == null || switchingOrigin) {
            return;
        }
        switchingOrigin = true;
        final int generation = ++switchGeneration;
        state = STATE_WEB;
        updateChrome();
        recreateWebViewForCleanup(generation);
        progressBar.setProgress(5);
        progressBar.setVisibility(View.VISIBLE);
    }

    /**
     * Commit a non-executable blank document before deleting cookies. destroy()
     * alone is asynchronous: the old renderer can still have cookie IPC queued.
     */
    private void recreateWebViewForCleanup(final int generation) {
        webViewGeneration++;
        currentMainFrameUrl = null;
        mainFrameLoading = false;
        progressBar.setVisibility(View.GONE);
        cancelActiveJsDialog();
        CookieManager.getInstance().setAcceptCookie(false);
        final WebView old = webView;
        if (old == null) {
            finishWebViewCleanup(null, generation);
            return;
        }
        old.stopLoading();
        old.getSettings().setJavaScriptEnabled(false);
        old.setWebChromeClient(null);
        old.setWebViewClient(new WebViewClient() {
            @Override
            public boolean shouldOverrideUrlLoading(WebView view, WebResourceRequest request) {
                return !"about:blank".equals(request.getUrl().toString());
            }

            @Override
            public void onPageFinished(WebView view, String url) {
                if ("about:blank".equals(url)) {
                    finishWebViewCleanup(view, generation);
                }
            }
        });
        old.loadUrl("about:blank");
    }

    private void finishWebViewCleanup(WebView old, int generation) {
        if (generation != switchGeneration || !switchingOrigin
                || isFinishing() || isDestroyed() || (old != null && old != webView)) {
            return;
        }
        webView = null;
        destroyWebView(old);
        WebView fresh = new WebView(this);
        fresh.setBackgroundColor(COLOR_SURFACE);
        webView = fresh;
        contentFrame.addView(fresh, 0, new FrameLayout.LayoutParams(MATCH, MATCH));
        configureWebView();
        updateChrome();
        clearWebDataThenLoad(generation);
    }

    private void destroyWebView(WebView target) {
        if (target == null) {
            return;
        }
        ViewGroup parent = target.getParent() instanceof ViewGroup
                ? (ViewGroup) target.getParent()
                : null;
        if (parent != null) {
            parent.removeView(target);
        }
        target.stopLoading();
        target.setWebChromeClient(null);
        target.setWebViewClient(null);
        target.destroy();
    }

    private void clearWebDataThenLoad(final int generation) {
        if (webView != null) {
            webView.stopLoading();
        }
        mainFrameLoading = false;
        WebStorage.getInstance().deleteAllData();
        if (webView != null) {
            webView.clearCache(true);
            webView.clearHistory();
            webView.clearFormData();
        }
        final CookieManager cookies = CookieManager.getInstance();
        cookies.removeAllCookies(cleared -> runOnUiThread(() -> {
            if (generation != switchGeneration) {
                return;
            }
            if (isFinishing() || isDestroyed()) {
                return;
            }
            cookies.flush();
            cookies.setAcceptCookie(true);
            // The old WebView is already destroyed; clear once more in case
            // storage or cache writes raced the asynchronous cookie clear.
            WebStorage.getInstance().deleteAllData();
            if (webView != null) {
                webView.clearCache(true);
                webView.clearHistory();
                webView.clearFormData();
            }
            prefs.edit().putBoolean(PREF_PENDING_CLEANUP, false).apply();
            switchingOrigin = false;
            saveButton.setEnabled(true);
            cancelButton.setEnabled(true);
            addressInput.setEnabled(true);
            settingsStatus.setVisibility(View.GONE);
            state = STATE_WEB;
            updateChrome();
            loadOrigin();
        }));
    }

    private void loadOrigin() {
        loadOrigin(false);
    }

    /**
     * @param bypassCaches when true, asks servers and intermediate caches to
     *                     revalidate the main document instead of reusing a
     *                     stored copy. Cookies, DOM storage, and the WebView
     *                     cache mode stay untouched.
     */
    private void loadOrigin(boolean bypassCaches) {
        if (committedOrigin == null || webView == null || switchingOrigin) {
            return;
        }
        if (state == STATE_ERROR) {
            state = STATE_WEB;
            updateChrome();
        }
        mainFrameLoading = true;
        currentMainFrameUrl = committedOrigin;
        progressBar.setProgress(5);
        if (state == STATE_WEB) {
            // A restore may load while the settings screen is showing; the
            // loading line must not be drawn over native UI.
            progressBar.setVisibility(View.VISIBLE);
        }
        if (bypassCaches) {
            Map<String, String> headers = new HashMap<>();
            headers.put("Cache-Control", "no-cache");
            headers.put("Pragma", "no-cache");
            webView.loadUrl(committedOrigin, headers);
        } else {
            webView.loadUrl(committedOrigin);
        }
    }

    /**
     * Settings action: drop cached page resources and reload the saved root
     * address. Cookies and web storage survive, so the login session is kept.
     * The address draft in the input field is intentionally ignored.
     */
    private void refreshPageCache() {
        if (switchingOrigin || committedOrigin == null || webView == null) {
            return;
        }
        hideKeyboard();
        clearUpdateState();
        cancelActiveJsDialog();
        webView.stopLoading();
        webView.clearCache(true);
        state = STATE_WEB;
        updateChrome();
        loadOrigin(true);
    }

    private void retryLoad() {
        if (switchingOrigin) {
            return;
        }
        if (committedOrigin == null) {
            enterSettings(STATE_WEB, true);
            return;
        }
        hideKeyboard();
        state = STATE_WEB;
        updateChrome();
        loadOrigin();
    }

    private void dismissError() {
        state = STATE_WEB;
        updateChrome();
    }

    private void showError(String title, String detail) {
        if (switchingOrigin) {
            return;
        }
        errorTitleText = title;
        errorDetailText = detail;
        if (errorTitleView != null) {
            errorTitleView.setText(title);
        }
        if (errorDetailView != null) {
            errorDetailView.setText(detail);
        }
        progressBar.setProgress(0);
        progressBar.setVisibility(View.GONE);
        mainFrameLoading = false;
        if (state == STATE_SETTINGS) {
            // Keep the settings screen usable; its "取消" returns to this error.
            settingsReturnState = STATE_ERROR;
            return;
        }
        state = STATE_ERROR;
        updateChrome();
    }

    private void applyErrorContent(String title, String detail) {
        String resolvedTitle = title != null ? title : "无法打开账本";
        String resolvedDetail = detail != null
                ? detail
                : "上次页面加载失败，请重试或检查服务器地址。";
        errorTitleText = resolvedTitle;
        errorDetailText = resolvedDetail;
        if (errorTitleView != null) {
            errorTitleView.setText(resolvedTitle);
        }
        if (errorDetailView != null) {
            errorDetailView.setText(resolvedDetail);
        }
    }

    private void showTlsError() {
        showError("证书校验失败",
                "为保护登录信息，应用已阻止连接到证书无效或不受信任的服务器。\n\n"
                        + "请确认服务器证书有效、域名匹配且设备时间正确后重试。\n\n目标："
                        + displayOrigin(committedOrigin));
    }

    private String displayOrigin(String origin) {
        return origin != null ? origin : "（未配置）";
    }

    private String networkErrorText(int code) {
        switch (code) {
            case WebViewClient.ERROR_HOST_LOOKUP:
                return "无法解析服务器地址，请检查地址拼写或网络连接。";
            case WebViewClient.ERROR_CONNECT:
                return "无法连接到服务器，请确认服务器已启动且网络可访问。";
            case WebViewClient.ERROR_TIMEOUT:
                return "连接服务器超时，请稍后重试。";
            case WebViewClient.ERROR_IO:
                return "网络连接中断，请检查网络后重试。";
            case WebViewClient.ERROR_PROXY_AUTHENTICATION:
                return "网络代理要求身份验证。";
            case WebViewClient.ERROR_UNSUPPORTED_SCHEME:
            case WebViewClient.ERROR_BAD_URL:
                return "服务器地址格式不受支持。";
            case WebViewClient.ERROR_REDIRECT_LOOP:
                return "服务器重定向次数过多。";
            case WebViewClient.ERROR_AUTHENTICATION:
                return "服务器要求身份验证。";
            case WebViewClient.ERROR_FAILED_SSL_HANDSHAKE:
                return "安全连接建立失败（TLS 握手失败）。";
            case WebViewClient.ERROR_UNSAFE_RESOURCE:
                return "页面包含不安全的资源，已阻止加载。";
            default:
                return "无法加载页面（错误码 " + code + "）。";
        }
    }

    private String httpReason(int status) {
        switch (status) {
            case 400:
                return "请求无效";
            case 401:
                return "需要登录或认证";
            case 403:
                return "没有访问权限";
            case 404:
                return "页面不存在";
            case 408:
                return "请求超时";
            case 429:
                return "请求过于频繁";
            case 500:
                return "服务器内部错误";
            case 502:
                return "网关或代理错误";
            case 503:
                return "服务暂时不可用";
            case 504:
                return "网关超时";
            default:
                return "无法完成请求";
        }
    }

    // ------------------------------------------------------------------
    // Application update
    // ------------------------------------------------------------------

    private final class UpdateListener implements AppUpdater.Listener {

        @Override
        public void onCheckFinished(AppUpdater.Metadata metadata, String error) {
            if (!isUpdateUiActive()) {
                return;
            }
            applyCheckResult(metadata, error);
        }

        @Override
        public void onDownloadProgress(long downloaded, long totalBytes) {
            if (!isUpdateUiActive() || updatePhase != UPDATE_DOWNLOADING) {
                return;
            }
            updateDownloaded = Math.max(0L, downloaded);
            if (totalBytes > 0) {
                updateTotalBytes = totalBytes;
            }
            refreshUpdateUi();
        }

        @Override
        public void onDownloadFinished(File apkFile, AppUpdater.Metadata metadata, String error) {
            if (!isUpdateUiActive()) {
                return;
            }
            updateDownloaded = 0;
            if (error != null || apkFile == null || metadata == null) {
                updateReadyFile = null;
                updatePhase = UPDATE_ERROR;
                updateMessage = error != null ? error : "下载更新失败，请重试。";
                refreshUpdateUi();
                return;
            }
            updateReadyFile = apkFile;
            updateMetadata = metadata;
            updatePhase = UPDATE_READY;
            updateMessage = "更新包已下载并通过完整性、版本与签名校验。";
            refreshUpdateUi();
            showInstallConfirm();
        }

        @Override
        public void onRevalidateFinished(File apkFile, String error) {
            if (!isUpdateUiActive()) {
                return;
            }
            if (error != null || apkFile == null) {
                if (updater != null) {
                    updater.discardReady();
                }
                updateReadyFile = null;
                updatePhase = UPDATE_ERROR;
                updateMessage = error != null ? error : "更新包已失效，请重新检查更新。";
                refreshUpdateUi();
                return;
            }
            installSystemPackage(apkFile);
        }
    }

    private boolean isUpdateUiActive() {
        return updateSection != null
                && updater != null
                && !isFinishing()
                && !isDestroyed()
                && state == STATE_SETTINGS;
    }

    private void refreshUpdateUi() {
        if (updateSection == null) {
            return;
        }
        boolean show = committedOrigin != null && !switchingOrigin;
        updateSection.setVisibility(show ? View.VISIBLE : View.GONE);
        if (!show) {
            if (updateCheckButton != null) {
                updateCheckButton.setEnabled(false);
            }
            if (updateCancelButton != null) {
                updateCancelButton.setVisibility(View.GONE);
            }
            if (updateProgress != null) {
                updateProgress.setVisibility(View.GONE);
            }
            return;
        }

        updateVersionView.setText("当前版本：" + installedVersionName()
                + "（" + installedVersionCode() + "）");

        boolean busy = updatePhase == UPDATE_CHECKING
                || updatePhase == UPDATE_DOWNLOADING
                || updatePhase == UPDATE_VALIDATING;
        if (updateCancelButton != null) {
            updateCancelButton.setVisibility(busy ? View.VISIBLE : View.GONE);
            updateCancelButton.setEnabled(busy);
        }
        if (updateProgress != null) {
            boolean showProgress = updatePhase == UPDATE_DOWNLOADING;
            updateProgress.setVisibility(showProgress ? View.VISIBLE : View.GONE);
            if (showProgress) {
                int percent = 0;
                if (updateTotalBytes > 0) {
                    percent = (int) Math.min(100L,
                            (updateDownloaded * 100L) / updateTotalBytes);
                }
                updateProgress.setProgress(percent);
            }
        }
        if (updateCheckButton != null) {
            updateCheckButton.setEnabled(!busy);
            switch (updatePhase) {
                case UPDATE_CHECKING:
                    updateCheckButton.setText("正在检查…");
                    break;
                case UPDATE_DOWNLOADING:
                    updateCheckButton.setText("正在下载…");
                    break;
                case UPDATE_VALIDATING:
                    updateCheckButton.setText("正在校验…");
                    break;
                case UPDATE_AVAILABLE:
                    updateCheckButton.setText("下载并安装");
                    break;
                case UPDATE_READY:
                    updateCheckButton.setText("安装更新");
                    break;
                case UPDATE_PERMISSION:
                    updateCheckButton.setText("继续安装");
                    break;
                case UPDATE_ERROR:
                    updateCheckButton.setText("重试");
                    break;
                default:
                    updateCheckButton.setText("检查更新");
                    break;
            }
        }

        updateStatusView.setText(updateStatusText());
        int color = COLOR_TEXT_SECONDARY;
        if (updatePhase == UPDATE_ERROR) {
            color = COLOR_ERROR;
        } else if (updatePhase == UPDATE_AVAILABLE
                || updatePhase == UPDATE_READY
                || updatePhase == UPDATE_PERMISSION) {
            color = COLOR_TEAL_DARK;
        }
        updateStatusView.setTextColor(color);
    }

    private String updateStatusText() {
        switch (updatePhase) {
            case UPDATE_CHECKING:
                return "正在检查更新…";
            case UPDATE_DOWNLOADING:
                int percent = updateTotalBytes > 0
                        ? (int) Math.min(100L, (updateDownloaded * 100L) / updateTotalBytes)
                        : 0;
                return "正在下载更新… " + percent + "%（"
                        + formatBytes(updateDownloaded) + " / "
                        + formatBytes(updateTotalBytes) + "）";
            case UPDATE_VALIDATING:
                return "正在校验安装包…";
            default:
                return updateMessage;
        }
    }

    @SuppressWarnings("deprecation")
    private long installedVersionCode() {
        if (installedVersionCodeCache >= 0) {
            return installedVersionCodeCache;
        }
        try {
            PackageInfo info = getPackageManager()
                    .getPackageInfo(getPackageName(), 0);
            long code = Build.VERSION.SDK_INT >= Build.VERSION_CODES.P
                    ? info.getLongVersionCode()
                    : info.versionCode;
            installedVersionCodeCache = code;
            return code;
        } catch (PackageManager.NameNotFoundException e) {
            return 0;
        }
    }

    private String installedVersionName() {
        if (installedVersionNameCache != null) {
            return installedVersionNameCache;
        }
        try {
            PackageInfo info = getPackageManager()
                    .getPackageInfo(getPackageName(), 0);
            String name = info.versionName != null ? info.versionName : "未知";
            installedVersionNameCache = name;
            return name;
        } catch (PackageManager.NameNotFoundException e) {
            return "未知";
        }
    }

    private String formatBytes(long bytes) {
        if (bytes < 1024L) {
            return bytes + " B";
        }
        double value = bytes / 1024.0;
        if (value < 1024.0) {
            return String.format(Locale.ROOT, "%.1f KB", value);
        }
        value /= 1024.0;
        if (value < 1024.0) {
            return String.format(Locale.ROOT, "%.1f MB", value);
        }
        return String.format(Locale.ROOT, "%.1f GB", value / 1024.0);
    }

    private void onUpdateAction() {
        switch (updatePhase) {
            case UPDATE_AVAILABLE:
                showDownloadConfirm();
                break;
            case UPDATE_READY:
                if (updateReadyFile != null) {
                    showInstallConfirm();
                }
                break;
            case UPDATE_PERMISSION:
                beginInstall();
                break;
            case UPDATE_IDLE:
            case UPDATE_UP_TO_DATE:
            case UPDATE_ERROR:
                startUpdateCheck();
                break;
            default:
                break;
        }
    }

    private void startUpdateCheck() {
        if (updater == null || committedOrigin == null || switchingOrigin) {
            return;
        }
        if (updatePhase == UPDATE_CHECKING
                || updatePhase == UPDATE_DOWNLOADING
                || updatePhase == UPDATE_VALIDATING) {
            return;
        }
        dismissUpdateDialog();
        hideKeyboard();
        updater.cancel();
        updater.discardReady();
        updateMetadata = null;
        updateReadyFile = null;
        updateDownloaded = 0;
        updateTotalBytes = 0;
        updateMessage = "";
        updatePhase = UPDATE_CHECKING;
        if (updateLatestView != null) {
            updateLatestView.setText("服务器最新版本：检查中…");
        }
        refreshUpdateUi();
        updater.check(committedOrigin);
    }

    private void applyCheckResult(AppUpdater.Metadata metadata, String error) {
        updateDownloaded = 0;
        updateTotalBytes = 0;
        if (error != null) {
            updateMetadata = null;
            updatePhase = UPDATE_ERROR;
            updateMessage = error;
            if (updateLatestView != null) {
                updateLatestView.setText("服务器最新版本：检查失败");
            }
            refreshUpdateUi();
            return;
        }
        if (metadata == null) {
            updateMetadata = null;
            updatePhase = UPDATE_UP_TO_DATE;
            updateMessage = "当前已是最新版本：服务器尚未发布新的安装包。";
            if (updateLatestView != null) {
                updateLatestView.setText("服务器最新版本：尚未发布");
            }
            refreshUpdateUi();
            return;
        }
        String latest = "服务器最新版本：" + metadata.versionName
                + "（" + metadata.versionCode + "）";
        if (updateLatestView != null) {
            updateLatestView.setText(latest);
        }
        if (metadata.versionCode <= installedVersionCode()) {
            updateMetadata = null;
            updatePhase = UPDATE_UP_TO_DATE;
            updateMessage = "当前已是最新版本（服务器版本 "
                    + metadata.versionName + "）。";
            refreshUpdateUi();
            return;
        }
        if (metadata.minSdk > Build.VERSION.SDK_INT) {
            updateMetadata = null;
            updatePhase = UPDATE_ERROR;
            updateMessage = "发现新版本 " + metadata.versionName
                    + "，但要求 Android API " + metadata.minSdk
                    + "；当前设备（API " + Build.VERSION.SDK_INT + "）暂不支持。";
            refreshUpdateUi();
            return;
        }
        updateMetadata = metadata;
        updatePhase = UPDATE_AVAILABLE;
        updateMessage = "发现新版本 " + metadata.versionName
                + "（" + metadata.versionCode + "），大小 "
                + formatBytes(metadata.sizeBytes) + "。";
        refreshUpdateUi();
        showDownloadConfirm();
    }

    private void showDownloadConfirm() {
        if (!isUpdateUiActive() || updateMetadata == null || originConfirmDialog != null) {
            return;
        }
        dismissUpdateDialog();
        final AppUpdater.Metadata metadata = updateMetadata;
        String message = "当前版本：" + installedVersionName()
                + "（" + installedVersionCode() + "）\n"
                + "新版本：" + metadata.versionName
                + "（" + metadata.versionCode + "）\n"
                + "下载大小约：" + formatBytes(metadata.sizeBytes)
                + "\n\n下载后会校验大小、SHA-256、包名、版本与签名，"
                + "然后交给 Android 系统确认覆盖安装。";
        AlertDialog dialog = new AlertDialog.Builder(this)
                .setTitle("发现新版本")
                .setMessage(message)
                .setNegativeButton("稍后", null)
                .setPositiveButton("下载", (d, which) -> {
                    updateDialog = null;
                    startUpdateDownload();
                })
                .create();
        dialog.setOnDismissListener(d -> {
            if (updateDialog == d) {
                updateDialog = null;
            }
        });
        updateDialog = dialog;
        dialog.show();
    }

    private void startUpdateDownload() {
        if (updater == null || committedOrigin == null || updateMetadata == null
                || switchingOrigin) {
            return;
        }
        updateDownloaded = 0;
        updateTotalBytes = updateMetadata.sizeBytes;
        updatePhase = UPDATE_DOWNLOADING;
        updateMessage = "";
        refreshUpdateUi();
        updater.download(committedOrigin, updateMetadata);
    }

    private void cancelUpdateTask() {
        if (updater == null || switchingOrigin) {
            return;
        }
        int phase = updatePhase;
        if (phase != UPDATE_CHECKING
                && phase != UPDATE_DOWNLOADING
                && phase != UPDATE_VALIDATING) {
            return;
        }
        updater.cancel();
        updateDownloaded = 0;
        if (phase == UPDATE_DOWNLOADING) {
            updatePhase = updateMetadata != null ? UPDATE_AVAILABLE : UPDATE_IDLE;
            updateMessage = "已取消下载，可点击「下载并安装」重试。";
        } else if (phase == UPDATE_CHECKING) {
            updateMetadata = null;
            updatePhase = UPDATE_IDLE;
            updateMessage = "已取消检查更新。";
            if (updateLatestView != null) {
                updateLatestView.setText("服务器最新版本：尚未检查");
            }
        } else {
            updatePhase = UPDATE_READY;
            updateMessage = "已取消校验。更新包仍保留，可点击「安装更新」重试。";
        }
        refreshUpdateUi();
    }

    private void clearUpdateState() {
        if (updater != null) {
            updater.cancel();
            updater.discardReady();
        }
        dismissUpdateDialog();
        awaitingInstallPermission = false;
        updateMetadata = null;
        updateReadyFile = null;
        updateDownloaded = 0;
        updateTotalBytes = 0;
        updateMessage = "尚未检查更新。";
        updatePhase = UPDATE_IDLE;
        if (updateLatestView != null) {
            updateLatestView.setText("服务器最新版本：尚未检查");
        }
        refreshUpdateUi();
    }

    private void showInstallConfirm() {
        if (!isUpdateUiActive() || updateMetadata == null || updateReadyFile == null
                || originConfirmDialog != null) {
            return;
        }
        dismissUpdateDialog();
        final AppUpdater.Metadata metadata = updateMetadata;
        String message = "新版本 " + metadata.versionName
                + "（" + metadata.versionCode + "）已下载并通过校验。\n\n"
                + "是否立即交给 Android 系统安装？系统会提示覆盖安装现有应用。";
        AlertDialog dialog = new AlertDialog.Builder(this)
                .setTitle("安装更新")
                .setMessage(message)
                .setNegativeButton("稍后", (d, which) -> {
                    updatePhase = UPDATE_READY;
                    updateMessage = "更新包已校验，可随时点击「安装更新」。";
                    refreshUpdateUi();
                })
                .setPositiveButton("安装", (d, which) -> {
                    updateDialog = null;
                    beginInstall();
                })
                .create();
        dialog.setOnDismissListener(d -> {
            if (updateDialog == d) {
                updateDialog = null;
            }
        });
        updateDialog = dialog;
        dialog.show();
    }

    private void beginInstall() {
        if (!isUpdateUiActive() || updateReadyFile == null || updateMetadata == null) {
            return;
        }
        dismissUpdateDialog();
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O
                && !getPackageManager().canRequestPackageInstalls()) {
            updatePhase = UPDATE_PERMISSION;
            updateMessage = "安装更新需要允许本应用「安装未知来源的应用」。"
                    + "请在系统设置中允许后返回。";
            refreshUpdateUi();
            openUnknownSourcesSettings();
            return;
        }
        startInstallValidation();
    }

    private void openUnknownSourcesSettings() {
        awaitingInstallPermission = true;
        Intent intent = new Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES,
                Uri.parse("package:" + getPackageName()));
        try {
            startActivity(intent);
        } catch (ActivityNotFoundException | SecurityException e) {
            try {
                startActivity(new Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES));
            } catch (ActivityNotFoundException | SecurityException fallbackError) {
                awaitingInstallPermission = false;
                updatePhase = UPDATE_ERROR;
                updateMessage = "无法打开系统安装权限设置。请手动在系统设置中允许"
                        + "本应用安装应用后重试。";
                refreshUpdateUi();
            }
        }
    }

    private void handleInstallPermissionReturn() {
        if (updater == null || isFinishing() || isDestroyed()) {
            return;
        }
        if (getPackageManager().canRequestPackageInstalls()) {
            if (updateReadyFile == null || updateMetadata == null) {
                updatePhase = UPDATE_ERROR;
                updateMessage = "更新包已不存在，请重新检查更新。";
                refreshUpdateUi();
                return;
            }
            startInstallValidation();
        } else {
            updatePhase = UPDATE_PERMISSION;
            updateMessage = "尚未允许本应用安装未知来源的应用。"
                    + "点击「继续安装」可再次打开系统设置。";
            refreshUpdateUi();
        }
    }

    private void startInstallValidation() {
        if (updater == null || updateReadyFile == null || updateMetadata == null) {
            return;
        }
        updatePhase = UPDATE_VALIDATING;
        updateMessage = "正在重新校验安装包…";
        refreshUpdateUi();
        updater.revalidateReady();
    }

    private void installSystemPackage(File apkFile) {
        if (apkFile == null || !apkFile.isFile()) {
            updatePhase = UPDATE_ERROR;
            updateMessage = "更新包已不存在，请重新检查更新。";
            refreshUpdateUi();
            return;
        }
        Uri uri = UpdateFileProvider.contentUri();
        Intent intent = new Intent(Intent.ACTION_VIEW);
        intent.setDataAndType(uri, "application/vnd.android.package-archive");
        intent.addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION);
        try {
            startActivity(intent);
            updatePhase = UPDATE_READY;
            updateMessage = "已请求 Android 系统安装更新，请在系统界面确认覆盖安装。";
        } catch (ActivityNotFoundException | SecurityException e) {
            updatePhase = UPDATE_ERROR;
            updateMessage = "无法启动系统安装程序，请确认设备允许安装应用后重试。";
        }
        refreshUpdateUi();
    }

    private void dismissUpdateDialog() {
        AlertDialog dialog = updateDialog;
        updateDialog = null;
        if (dialog != null && dialog.isShowing()) {
            dialog.dismiss();
        }
    }

    // ------------------------------------------------------------------
    // Back handling
    // ------------------------------------------------------------------

    private void registerBackCallback() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            registerBackCallbackApi33();
        }
    }

    @android.annotation.TargetApi(Build.VERSION_CODES.TIRAMISU)
    private void registerBackCallbackApi33() {
        OnBackInvokedCallback callback = this::handleBack;
        backCallback = callback;
        getOnBackInvokedDispatcher().registerOnBackInvokedCallback(
                OnBackInvokedDispatcher.PRIORITY_DEFAULT, callback);
    }

    @android.annotation.TargetApi(Build.VERSION_CODES.TIRAMISU)
    private void unregisterBackCallbackApi33() {
        if (backCallback instanceof OnBackInvokedCallback) {
            getOnBackInvokedDispatcher().unregisterOnBackInvokedCallback(
                    (OnBackInvokedCallback) backCallback);
            backCallback = null;
        }
    }

    @Override
    @SuppressWarnings("deprecation")
    public void onBackPressed() {
        handleBack();
    }

    private void handleBack() {
        if (state == STATE_SETTINGS) {
            if (switchingOrigin) {
                return;
            }
            cancelSettings();
            return;
        }
        if (state == STATE_ERROR) {
            dismissError();
            return;
        }
        if (webView != null && webView.canGoBack()) {
            webView.goBack();
            return;
        }
        finish();
    }

    // ------------------------------------------------------------------
    // Lifecycle
    // ------------------------------------------------------------------

    private void hideKeyboard() {
        View focused = getCurrentFocus();
        if (focused == null) {
            focused = addressInput;
        }
        if (focused != null) {
            InputMethodManager imm =
                    (InputMethodManager) getSystemService(INPUT_METHOD_SERVICE);
            if (imm != null) {
                imm.hideSoftInputFromWindow(focused.getWindowToken(), 0);
            }
        }
    }

    @Override
    protected void onSaveInstanceState(Bundle outState) {
        super.onSaveInstanceState(outState);
        outState.putString(KEY_SAVED_ORIGIN, committedOrigin);
        String viewOrigin = webView != null
                ? ServerAddress.originOf(webView.getUrl())
                : null;
        outState.putString(KEY_VIEW_ORIGIN, viewOrigin);
        outState.putInt(KEY_UI_STATE, state);
        outState.putInt(KEY_RETURN_STATE, settingsReturnState);
        outState.putString(KEY_DRAFT,
                addressInput != null ? addressInput.getText().toString() : "");
        outState.putString(KEY_ERROR_TITLE, errorTitleText);
        outState.putString(KEY_ERROR_DETAIL, errorDetailText);
        if (webView != null) {
            webView.saveState(outState);
        }
    }

    @Override
    protected void onPause() {
        super.onPause();
        if (webView != null) {
            webView.onPause();
        }
        // Persist session cookies across process restarts.
        CookieManager.getInstance().flush();
    }

    @Override
    protected void onResume() {
        super.onResume();
        if (webView != null) {
            webView.onResume();
        }
        if (awaitingInstallPermission) {
            awaitingInstallPermission = false;
            handleInstallPermissionReturn();
        }
    }

    @Override
    protected void onDestroy() {
        switchGeneration++;
        webViewGeneration++;
        WebView oldWebView = webView;
        webView = null;
        currentMainFrameUrl = null;
        mainFrameLoading = false;
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU
                && backCallback != null) {
            unregisterBackCallbackApi33();
        }
        cancelActiveJsDialog();
        if (originConfirmDialog != null) {
            originConfirmDialog.dismiss();
            originConfirmDialog = null;
        }
        dismissUpdateDialog();
        awaitingInstallPermission = false;
        if (updater != null) {
            updater.release();
            updater = null;
        }
        destroyWebView(oldWebView);
        CookieManager.getInstance().flush();
        super.onDestroy();
    }
}
