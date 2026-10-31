package com.household.bills;

import android.content.ContentProvider;
import android.content.ContentValues;
import android.content.Context;
import android.database.Cursor;
import android.database.MatrixCursor;
import android.net.Uri;
import android.os.ParcelFileDescriptor;
import android.provider.OpenableColumns;

import java.io.File;
import java.io.FileNotFoundException;
import java.io.IOException;

/**
 * Exposes exactly one application-private staged APK to the system installer.
 *
 * <p>The provider is not exported, accepts only the single fixed
 * {@code updates/app-update.apk} path, and only opens it read-only. Other apps
 * gain access solely through the temporary read grant attached to the
 * installer intent.</p>
 */
public final class UpdateFileProvider extends ContentProvider {

    static final String AUTHORITY = "com.household.bills.updatefiles";

    private static final String DIRECTORY_NAME = "updates";
    private static final String FILE_NAME = "app-update.apk";
    private static final String MIME_TYPE = "application/vnd.android.package-archive";
    private static final String EXPECTED_PATH = "/" + DIRECTORY_NAME + "/" + FILE_NAME;

    static Uri contentUri() {
        return new Uri.Builder()
                .scheme("content")
                .authority(AUTHORITY)
                .appendPath(DIRECTORY_NAME)
                .appendPath(FILE_NAME)
                .build();
    }

    static File stagedFile(Context context) {
        return new File(new File(context.getCacheDir(), DIRECTORY_NAME), FILE_NAME);
    }

    @Override
    public boolean onCreate() {
        return true;
    }

    @Override
    public ParcelFileDescriptor openFile(Uri uri, String mode) throws FileNotFoundException {
        if (!"r".equals(mode) && !"rt".equals(mode)) {
            throw new FileNotFoundException("更新包仅支持只读访问");
        }
        File file = resolve(uri);
        return ParcelFileDescriptor.open(file, ParcelFileDescriptor.MODE_READ_ONLY);
    }

    @Override
    public String getType(Uri uri) {
        try {
            resolve(uri);
            return MIME_TYPE;
        } catch (FileNotFoundException e) {
            return null;
        }
    }

    @Override
    public Cursor query(Uri uri, String[] projection, String selection,
                        String[] selectionArgs, String sortOrder) {
        final File file;
        try {
            file = resolve(uri);
        } catch (FileNotFoundException e) {
            return null;
        }
        String[] requested = projection != null ? projection
                : new String[]{OpenableColumns.DISPLAY_NAME, OpenableColumns.SIZE};
        MatrixCursor cursor = new MatrixCursor(requested, 1);
        Object[] row = new Object[requested.length];
        for (int i = 0; i < requested.length; i++) {
            if (OpenableColumns.DISPLAY_NAME.equals(requested[i])) {
                row[i] = FILE_NAME;
            } else if (OpenableColumns.SIZE.equals(requested[i])) {
                row[i] = file.length();
            }
        }
        cursor.addRow(row);
        return cursor;
    }

    @Override
    public Uri insert(Uri uri, ContentValues values) {
        throw new UnsupportedOperationException("更新文件不可写入");
    }

    @Override
    public int delete(Uri uri, String selection, String[] selectionArgs) {
        throw new UnsupportedOperationException("更新文件不可删除");
    }

    @Override
    public int update(Uri uri, ContentValues values, String selection, String[] selectionArgs) {
        throw new UnsupportedOperationException("更新文件不可修改");
    }

    private File resolve(Uri uri) throws FileNotFoundException {
        if (uri == null
                || !"content".equals(uri.getScheme())
                || !AUTHORITY.equals(uri.getAuthority())
                || uri.getQuery() != null
                || uri.getFragment() != null
                || !EXPECTED_PATH.equals(uri.getPath())) {
            throw new FileNotFoundException("更新文件路径不受支持");
        }
        Context context = getContext();
        if (context == null) {
            throw new FileNotFoundException("更新文件不可用");
        }
        File directory = new File(context.getCacheDir(), DIRECTORY_NAME);
        File file = new File(directory, FILE_NAME);
        try {
            String basePath = directory.getCanonicalPath();
            String filePath = file.getCanonicalPath();
            if (!filePath.equals(new File(basePath, FILE_NAME).getCanonicalPath())) {
                throw new FileNotFoundException("更新文件路径不受支持");
            }
        } catch (IOException e) {
            throw new FileNotFoundException("更新文件路径不受支持");
        }
        if (!file.isFile()) {
            throw new FileNotFoundException("更新文件不存在");
        }
        return file;
    }
}
