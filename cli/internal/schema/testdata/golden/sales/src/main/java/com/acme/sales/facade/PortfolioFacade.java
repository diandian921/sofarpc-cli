package com.acme.sales.facade;

import com.acme.sales.dto.AssetDTO;
import com.acme.sales.dto.AssetQuery;
import com.acme.sales.dto.Result;
import java.util.List;
import java.util.Map;
import javax.validation.constraints.NotNull;

/** 组合资产服务 */
public interface PortfolioFacade {
    /** 查询最新资产 */
    Result<List<AssetDTO>> queryPortfolioLatestAsset(@NotNull AssetQuery request, Map<String, List<Long>> filters);
}
