package com.acme.modern.dto;

import java.math.BigDecimal;
import java.util.List;
import javax.validation.constraints.NotNull;

public record PositionRecord(
        @NotNull Long id,
        BigDecimal amount,
        List<String> tags) {
}
