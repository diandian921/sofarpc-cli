package com.acme.facade;

import com.acme.dto.QueryRequest;
import com.acme.dto.QueryResponse;
import com.acme.dto.wildcard.*;
import static com.acme.util.Helpers.format;
import java.util.List;
import java.util.Map;

/**
 * 资产查询门面。
 */
@Deprecated
public interface AssetFacade<T extends Number> {
    /** 查询资产 */
    QueryResponse<List<Asset>> queryAssets(
            @NotNull QueryRequest request,
            Map<String, List<Long>> filters,
            int limit
    );

    /** 兜底 */
    default boolean ping() {
        return true;
    }

    <K> Map<K, List<T>> wrap(K key);

    enum Tier {
        BRONZE, SILVER, GOLD;
    }

    record Page<U>(int offset, int limit, List<U> items) {}

    @interface Cached {
        int ttl() default 60;
    }

    class Default {
        private final String name = "x";
        public String name() { return name; }
    }
}
