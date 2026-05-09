package com.example;

import java.util.List;

public class Dog extends Animal implements Trainable {

    public Dog(String name) {
        super(name);
    }

    @Override
    public String sound() {
        return "woof";
    }

    @Override
    public void learn(List<String> tricks) {
        for (String trick : tricks) {
            System.out.println(name + " learned " + trick);
        }
    }
}
