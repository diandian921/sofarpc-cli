package com.acme.wildcard.facade;

import com.acme.wildcard.dto.*;

/**
 * Wildcard import facade.
 */
public interface WildcardFacade {
    /** 查询资产 */
    WildResp query(WildReq req);
}
