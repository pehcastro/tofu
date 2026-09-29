const fs = require('node:fs');
const vm = require('node:vm');

const [expressionFile, quietAfter] = process.argv.slice(2);
const observers = [];
class MutationObserver {
  constructor(callback) {
    this.callback = callback;
    observers.push(this);
  }
  observe() {}
  disconnect() {
    this.off = true;
  }
}
const started = performance.now();
const mutating = setInterval(() => {
  if (quietAfter !== 'never' && performance.now() - started >= Number(quietAfter)) return clearInterval(mutating);
  for (const observer of observers) if (!observer.off) observer.callback([]);
}, 40);
const context = vm.createContext({MutationObserver, performance, setTimeout, clearTimeout, document: {readyState: 'complete'}, URL});
Promise.resolve(vm.runInContext(fs.readFileSync(expressionFile, 'utf8'), context)).then(settled => {
  console.log(JSON.stringify({settled, wall: Math.round(performance.now() - started)}));
  process.exit(0);
});
