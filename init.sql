-- 初期データベーススキーマとデータの作成

-- ユーザーテーブル
CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    email VARCHAR(150) UNIQUE NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 投稿テーブル
CREATE TABLE posts (
    id SERIAL PRIMARY KEY,
    user_id INTEGER REFERENCES users(id),
    title VARCHAR(200) NOT NULL,
    content TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- コメントテーブル
CREATE TABLE comments (
    id SERIAL PRIMARY KEY,
    post_id INTEGER REFERENCES posts(id),
    author_name VARCHAR(100) NOT NULL,
    content TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 初期データの挿入
INSERT INTO users (name, email) VALUES
    ('Alice Johnson', 'alice@example.com'),
    ('Bob Smith', 'bob@example.com'),
    ('Carol Davis', 'carol@example.com'),
    ('David Wilson', 'david@example.com'),
    ('Eve Brown', 'eve@example.com');

INSERT INTO posts (user_id, title, content) VALUES
    (1, 'Getting Started with OpenTelemetry', 'OpenTelemetry is a powerful observability framework that helps you instrument your applications...'),
    (2, 'Database Performance Tuning', 'When working with databases, performance is crucial. Here are some tips for optimizing your queries...'),
    (3, 'Microservices Architecture Patterns', 'Microservices have become increasingly popular, but they come with their own set of challenges...'),
    (1, 'Monitoring Distributed Systems', 'In a distributed system, monitoring becomes much more complex than in monolithic applications...'),
    (4, 'Container Orchestration Best Practices', 'When deploying containerized applications, orchestration platforms like Kubernetes provide...'),
    (5, 'API Design Guidelines', 'Well-designed APIs are the backbone of modern applications. Here are some guidelines to follow...'),
    (2, 'PostgreSQL Advanced Features', 'PostgreSQL offers many advanced features that can help optimize your database operations...'),
    (3, 'Tracing in Go Applications', 'Adding distributed tracing to Go applications can provide valuable insights into performance...');

INSERT INTO comments (post_id, author_name, content) VALUES
    (1, 'Tech Enthusiast', 'Great introduction to OpenTelemetry! Very helpful for beginners.'),
    (1, 'Developer123', 'I would love to see more examples with different programming languages.'),
    (2, 'DBA_Expert', 'These performance tips are spot on. Especially the indexing advice.'),
    (3, 'Architect_Pro', 'Microservices are indeed challenging. Service mesh can help with some of these issues.'),
    (1, 'StudentCoder', 'This helped me understand the basics. Thanks for sharing!'),
    (4, 'DevOps_Guru', 'Monitoring is crucial. What tools do you recommend for alerting?'),
    (5, 'K8s_Admin', 'Container orchestration can be overwhelming at first, but these practices help a lot.'),
    (6, 'API_Designer', 'REST vs GraphQL is always a hot topic. Both have their place.'),
    (2, 'Performance_Fan', 'Have you tried using connection pooling? It can make a huge difference.'),
    (7, 'PostgresLover', 'PostgreSQL is so powerful! The JSON support is particularly impressive.'),
    (8, 'Go_Developer', 'Tracing in Go is getting better with each release. Great timing for this post.'),
    (3, 'Cloud_Native', 'Service discovery and load balancing are also important considerations.');