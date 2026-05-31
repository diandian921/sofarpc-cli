package com.acme.modifiers.facade;

import com.acme.modifiers.dto.AccountDTO;

public interface AccountFacade {
    /** 查询账户 */
    AccountDTO getAccount(Long mpCode);
}
