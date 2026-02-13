<markdown-generation>
<instruction>When generating markdown text, use 4 backticks instead of 3 to avoid creating unbalanced code regions where the text looks broken because the code regions are opening and closing out of sync. Use GitHub Flavored Markdown.</instruction>
<example>
<correct>
````markdown
# Example
```javascript
console.log('hello');
```
````
</correct>
<incorrect>
```markdown
# Example
```javascript
console.log('hello');
```
```
</incorrect>
</example>
</markdown-generation>
