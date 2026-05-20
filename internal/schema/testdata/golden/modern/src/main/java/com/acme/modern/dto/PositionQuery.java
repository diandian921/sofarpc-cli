package com.acme.modern.dto;

import java.math.BigDecimal;
import java.util.List;
import java.util.Map;
import lombok.Builder;
import lombok.Data;

@Data
@Builder
public class PositionQuery {
    /** 产品代码 */
    private Long mpCode;

    @Deprecated
    private List<String> states;

    private PositionStatus status;

    private Map<String, List<BigDecimal>> amountFilters;
}
