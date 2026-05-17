package com.sofarpc.daemon.rpc;

import com.alipay.sofa.rpc.api.GenericService;
import com.alipay.sofa.rpc.context.RpcInvokeContext;
import org.junit.After;
import org.junit.Test;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertTrue;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.ArgumentMatchers.anyString;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.when;

/**
 * Pins that the per-call timeout travels via RpcInvokeContext, not the consumer
 * config. This is what lets one cached consumer serve every timeout, keeping the
 * cache warm and bounded.
 *
 * @author wuwh
 */
public class SofaRpcGatewayTest {

    @After
    public void clearContext() {
        RpcInvokeContext.removeContext();
    }

    @Test
    public void appliesPerCallTimeoutViaRpcInvokeContext() {
        final Integer[] timeoutAtInvoke = new Integer[1];
        GenericService svc = mock(GenericService.class);
        when(svc.$genericInvoke(anyString(), any(String[].class), any(Object[].class)))
                .thenAnswer(inv -> {
                    timeoutAtInvoke[0] = RpcInvokeContext.getContext().getTimeout();
                    return "OK";
                });
        ConsumerFactory factory = (key, connectTimeoutMs) -> new ManagedConsumer() {
            @Override
            public GenericService service() {
                return svc;
            }

            @Override
            public void close() {
            }
        };
        SofaRpcGateway gateway = new SofaRpcGateway(new ConnectionManager(256, factory));
        RpcCallSpec spec = new RpcCallSpec(
                "10.0.0.1:12200",
                "com.company.user.api.UserQueryService",
                "queryUser",
                new String[]{"java.lang.String"},
                new Object[]{"123"},
                4321);

        RpcInvokeContext.removeContext();
        RpcCallResult result = gateway.call(spec);

        assertTrue(result.isOk());
        assertTrue(timeoutAtInvoke[0] <= 4321);
        assertTrue(timeoutAtInvoke[0] > 0);
    }

    @Test
    public void coldConsumerCreationUsesRemainingTimeoutBudget() {
        final Integer[] connectTimeout = new Integer[1];
        GenericService svc = mock(GenericService.class);
        when(svc.$genericInvoke(anyString(), any(String[].class), any(Object[].class))).thenReturn("OK");
        ConsumerFactory factory = (key, connectTimeoutMs) -> {
            connectTimeout[0] = connectTimeoutMs;
            return new ManagedConsumer() {
                @Override
                public GenericService service() {
                    return svc;
                }

                @Override
                public void close() {
                }
            };
        };
        SofaRpcGateway gateway = new SofaRpcGateway(new ConnectionManager(256, factory));
        RpcCallSpec spec = new RpcCallSpec(
                "10.0.0.1:12200",
                "com.company.user.api.UserQueryService",
                "queryUser",
                new String[]{"java.lang.String"},
                new Object[]{"123"},
                500);

        RpcCallResult result = gateway.call(spec);

        assertTrue(result.isOk());
        assertTrue("connect timeout should not exceed call budget", connectTimeout[0] <= 500);
        assertTrue(connectTimeout[0] > 0);
    }
}
