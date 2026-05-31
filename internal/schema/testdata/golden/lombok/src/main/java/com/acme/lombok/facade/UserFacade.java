package com.acme.lombok.facade;

import com.acme.lombok.dto.UserDTO;

public interface UserFacade {
    /** 查用户 */
    UserDTO getUser(Long userId);
}
