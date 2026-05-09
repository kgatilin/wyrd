package com.example;

import java.util.List;
import java.util.Objects;

/**
 * Drives the call-resolution test fixture. Each method exercises one
 * resolution scenario:
 *
 * <ul>
 *   <li>{@link #register(String)} — same-source field-typed receiver →
 *       resolves to {@code com.example.UserRepository}.</li>
 *   <li>{@link #ping(String)} — stdlib + unqualified-stdlib calls → external,
 *       captured under {@code unresolved}.</li>
 *   <li>{@link #scheduleAll(List)} — anonymous-class body wraps a stdlib call
 *       that must not be attributed to the enclosing method.</li>
 *   <li>{@link #lambdaWalk(List)} — lambda body wraps both an in-source and a
 *       stdlib call; neither belongs to the enclosing method.</li>
 * </ul>
 */
public class UserService {

    private final UserRepository repo;

    public UserService(UserRepository repo) {
        this.repo = Objects.requireNonNull(repo);
    }

    public void register(String id) {
        // Resolved: receiver `repo` is a field of type UserRepository,
        // declared in this src-root. target_fqn should be
        // com.example.UserRepository.
        repo.save(id);
    }

    public String ping(String id) {
        // Unresolved: System.out is stdlib (java.io.PrintStream).
        System.out.println(id);
        // Unresolved: Objects.toString is stdlib (java.util.Objects).
        return Objects.toString(id);
    }

    public void scheduleAll(List<String> ids) {
        // Anonymous-class body: the println call must NOT appear under
        // scheduleAll's calls — it belongs to the synthesised Runnable.run.
        Runnable r = new Runnable() {
            @Override
            public void run() {
                System.out.println("inner");
            }
        };
        // Same-source call inside the enclosing method body — this one
        // SHOULD appear under scheduleAll.
        repo.save("schedule");
    }

    public int lambdaWalk(List<String> ids) {
        // Lambda body: forEach's argument body is its own executable scope,
        // so neither repo.find nor System.out.println should appear under
        // lambdaWalk's calls.
        ids.forEach(id -> {
            repo.find(id);
            System.out.println(id);
        });
        // Direct call inside the enclosing method — does belong here. Stays
        // unresolved because List<String> resolves to java.util.List.
        return ids.size();
    }
}
