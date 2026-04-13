package com.sofarpc.cli.core;

/**
 * Unified exit code constants for all CLI commands.
 *
 * @author wuwh
 */
public final class ExitCodes {

    public static final int SUCCESS = 0;

    // Invocation failure or assertion failure
    public static final int INVOKE_FAIL = 1;

    // Connection refused, timeout, no available provider
    public static final int CONNECT_FAIL = 2;

    // Invalid arguments or argument parsing failure
    public static final int BAD_ARGS = 3;

    // Server alias not found in servers.yaml
    public static final int ALIAS_NOT_FOUND = 4;

    private ExitCodes() {
    }
}
