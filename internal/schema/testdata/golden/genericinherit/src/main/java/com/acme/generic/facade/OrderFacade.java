package com.acme.generic.facade;

import com.acme.generic.dto.OrderDTO;

public interface OrderFacade {
    /** 建单 */
    OrderDTO createOrder(OrderDTO order);
}
