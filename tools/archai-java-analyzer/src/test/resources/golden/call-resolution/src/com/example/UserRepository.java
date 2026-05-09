package com.example;

/**
 * Same-source dependency target — exercises target_fqn resolution from a
 * field-typed receiver.
 */
public class UserRepository {

    public void save(String id) {
        // body content irrelevant — only its presence as a target matters
    }

    public String find(String id) {
        return id;
    }
}
