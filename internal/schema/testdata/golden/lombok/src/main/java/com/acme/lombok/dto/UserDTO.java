package com.acme.lombok.dto;

import lombok.Data;
import lombok.Builder;
import com.alibaba.fastjson.annotation.JSONField;

@Data
@Builder
public class UserDTO {
    @JSONField(name = "user_id")
    private Long userId;
    private String name;
    private transient String token;
    private static final long serialVersionUID = 1L;
}
