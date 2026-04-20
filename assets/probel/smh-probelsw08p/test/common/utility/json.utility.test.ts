import 'reflect-metadata';

import { JsonUtility } from '../../../src/common/utility/json.utility';

class JsonUtilityEx extends JsonUtility {
    constructor() {
        super();
    }
}

describe('JsonUtility', () => {
    it('should throw an error when ctor is called', () => {
        // Arrange

        // Act
        const util = (): JsonUtilityEx => new JsonUtilityEx();

        // Assert
        expect(util).toThrowError();
    });

    it('should convert object with circular dependencies to string', () => {
        // Arrange
        const address = { customer: <any>null };
        address.customer = { address: address };

        // Act
        const result = JsonUtility.stringify(address);
        // Assert
        expect(result).toBe(
            `{
  "customer": {
    "address": "~"
  }
}`
        );
    });

    it('should convert a map', () => {
        // Arrange
        const map = new Map<string, string>();
        map.set('key1', 'value1');
        map.set('key1', 'value1');

        // Act
        const result = JsonUtility.stringifyMap(map);

        // Assert
        expect(result).not.toBeNull();
    });

    it('should convert a map', () => {
        // Arrange
        const map = new Map<string, string>();
        map.set('key1', 'value1');
        map.set('key2', 'value2');

        // Act
        const result = JsonUtility.stringifyMap(map);

        // Assert
        expect(result).not.toBe(
            `[{
                "key1": "1",
                "key2": "2"
              },"value1","value2"]`
        );
    });

    it('should convert a map of map', () => {
        // Arrange
        const mapParent = new Map<string, Map<string, string>>();
        const mapChild = new Map<string, string>();
        mapChild.set('c_key1', 'c_value');
        mapParent.set('key1', mapChild);

        // Act
        const result = JsonUtility.stringifyMap(mapParent);

        // Assert
        expect(result).not.toBe(
            `{
                "key1": {
                  "c_key1": "c_value"
                }
              }`
        );
    });

    it('should safely parse an object', () => {
        // Arrange
        const data = {
            name: 'test'
        };

        // Act
        const result = JsonUtility.safeJSONParse(JSON.stringify(data));

        // Assert
        expect(result).toBeDefined();
        expect(result.name).toBe(data.name);
    });

    it('should safely parse an invalid object', () => {
        // Arrange
        const data = 'test';

        // Act
        const result = JsonUtility.safeJSONParse(data);

        // Assert
        expect(result).toBeUndefined();
    });
});
