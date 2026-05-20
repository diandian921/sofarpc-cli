package com.acme.modern.facade;

import com.acme.modern.dto.Page;
import com.acme.modern.dto.PositionQuery;
import com.acme.modern.dto.PositionRecord;
import com.acme.modern.dto.Result;
import java.util.List;
import javax.validation.Valid;
import org.jetbrains.annotations.Nullable;

public interface PositionFacade {
    /**
     * 查询持仓快照
     */
    @Deprecated
    Result<Page<PositionRecord>> queryPositions(
            @Valid final PositionQuery query,
            @Nullable("optional account ids") List<Long> accountIds);
}
