package com.example.app;

import com.example.service.Greeter;
import com.example.repo.Storage;

public class App {
    private final Greeter greeter;
    private final Storage storage;

    public App(Greeter greeter, Storage storage) {
        this.greeter = greeter;
        this.storage = storage;
    }

    public String run(String name) {
        String greeting = greeter.greet(name);
        storage.save(name, greeting);
        return greeting;
    }
}
