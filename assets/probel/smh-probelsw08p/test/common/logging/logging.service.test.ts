import 'reflect-metadata';

import { LoggingService } from '../../../src/common/logging/logging.service';

describe('LoggingService', () => {
    it('should error a message', () => {
        // Arrange
        console.error = jest.fn();

        // Act
        new LoggingService().error(() => 'sample message');

        // Assert
        expect(console.error).toHaveBeenCalledTimes(1);
    });

    it('should warn a message', () => {
        // Arrange
        console.log = jest.fn();

        // Act
        new LoggingService().warn(() => 'sample message');

        // Assert
        expect(console.log).toHaveBeenCalledTimes(1);
    });

    it('should info a message', () => {
        // Arrange
        console.log = jest.fn();

        // Act
        new LoggingService().info(() => 'sample message');

        // Assert
        expect(console.log).toHaveBeenCalledTimes(1);
    });

    it('should debug a message', () => {
        // Arrange
        console.log = jest.fn();

        // Act
        new LoggingService().debug(() => 'sample message');

        // Assert
        expect(console.log).toHaveBeenCalledTimes(1);
    });

    it('should trace a message', () => {
        // Arrange
        console.log = jest.fn();

        // Act
        new LoggingService().trace(() => 'sample message');

        // Assert
        expect(console.log).toHaveBeenCalledTimes(1);
    });
});
