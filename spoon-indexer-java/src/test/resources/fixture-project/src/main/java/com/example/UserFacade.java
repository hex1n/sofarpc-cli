package com.example;

public interface UserFacade {
    /**
     * @param request 必传 用户请求
     */
    ResponseEnvelope getUser(UserRequest request);
}
