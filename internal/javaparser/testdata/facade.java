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
public interface AssetFacade {
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
}
