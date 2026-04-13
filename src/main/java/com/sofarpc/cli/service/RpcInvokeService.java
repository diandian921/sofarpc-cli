package com.sofarpc.cli.service;

import com.alipay.sofa.rpc.api.GenericService;
import com.sofarpc.cli.core.ExceptionClassifier;
import com.sofarpc.cli.core.ExitCodes;
import com.sofarpc.cli.core.RpcClientFactory;
import com.sofarpc.cli.core.ServerStore;

import java.io.IOException;

/**
 * Core RPC invocation service. Encapsulates server resolution, generic invocation,
 * result flattening, and exception classification.
 *
 * @author wuwh
 */
public class RpcInvokeService {

    private final ServerStore serverStore;

    public RpcInvokeService() {
        this.serverStore = new ServerStore();
    }

    public RpcInvokeService(ServerStore serverStore) {
        this.serverStore = serverStore;
    }

    /**
     * Execute an RPC invocation via generic service.
     *
     * @param serverAlias server alias name
     * @param service     fully qualified interface name
     * @param method      method name
     * @param argTypes    parameter type names
     * @param args        parameter values
     * @param timeout     timeout in milliseconds
     * @return structured invocation result
     */
    public InvokeResult invoke(String serverAlias, String service, String method,
                               String[] argTypes, Object[] args, int timeout) {
        String address;
        try {
            address = serverStore.resolveAddress(serverAlias);
        } catch (IOException e) {
            return InvokeResult.failure(
                "读取服务配置失败: " + e.getMessage(), 0, ExitCodes.BAD_ARGS);
        }
        if (address == null) {
            return InvokeResult.aliasNotFound(serverAlias);
        }
        return invokeWithAddress(serverAlias, address, service, method, argTypes, args, timeout);
    }

    /**
     * Execute an RPC invocation with a pre-resolved address.
     * Used by BatchCommand where server is resolved once for all cases.
     *
     * @param serverAlias server alias name (for cache key)
     * @param address     resolved bolt address
     * @param service     fully qualified interface name
     * @param method      method name
     * @param argTypes    parameter type names
     * @param args        parameter values
     * @param timeout     timeout in milliseconds
     * @return structured invocation result
     */
    public InvokeResult invokeWithAddress(String serverAlias, String address, String service,
                                          String method, String[] argTypes, Object[] args,
                                          int timeout) {
        long start = System.currentTimeMillis();
        try {
            GenericService genericService = RpcClientFactory.getOrCreate(
                serverAlias, address, service, timeout);
            Object rawResult = genericService.$genericInvoke(method, argTypes, args);
            Object result = RpcClientFactory.flattenResult(rawResult);
            long latencyMs = System.currentTimeMillis() - start;
            return InvokeResult.success(result, latencyMs);
        } catch (Exception e) {
            long latencyMs = System.currentTimeMillis() - start;
            int exitCode = ExceptionClassifier.classify(e);
            String error = e.getMessage() != null ? e.getMessage() : e.getClass().getSimpleName();
            return InvokeResult.failure(error, latencyMs, exitCode);
        }
    }

    /**
     * Structured result of an RPC invocation.
     */
    public static class InvokeResult {
        private final boolean success;
        private final long latencyMs;
        private final Object result;
        private final String error;
        private final int exitCode;

        private InvokeResult(boolean success, long latencyMs, Object result,
                             String error, int exitCode) {
            this.success = success;
            this.latencyMs = latencyMs;
            this.result = result;
            this.error = error;
            this.exitCode = exitCode;
        }

        public static InvokeResult success(Object result, long latencyMs) {
            return new InvokeResult(true, latencyMs, result, null, ExitCodes.SUCCESS);
        }

        public static InvokeResult failure(String error, long latencyMs, int exitCode) {
            return new InvokeResult(false, latencyMs, null, error, exitCode);
        }

        public static InvokeResult aliasNotFound(String alias) {
            return new InvokeResult(false, 0, null,
                "服务别名不存在: " + alias, ExitCodes.ALIAS_NOT_FOUND);
        }

        public boolean isSuccess() {
            return success;
        }

        public long getLatencyMs() {
            return latencyMs;
        }

        public Object getResult() {
            return result;
        }

        public String getError() {
            return error;
        }

        public int getExitCode() {
            return exitCode;
        }
    }
}
