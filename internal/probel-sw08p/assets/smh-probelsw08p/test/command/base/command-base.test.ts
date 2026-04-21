import { CrossPointInterrogateMessageCommand } from '../../../src/command/rx/001-crosspoint-interrogate-message/command';
import { CrossPointInterrogateMessageCommandParams, } from '../../../src/command/rx/001-crosspoint-interrogate-message/params';
import { DisplayCommand } from '../../../src/command/command-contract';
import { CommandBase } from '../../../src/command/command.base';


describe('CommandBase', () => {
    let command: CommandBase<CrossPointInterrogateMessageCommandParams,null>;

    beforeAll(() => {
        const params: CrossPointInterrogateMessageCommandParams = {
            matrixId: 0,
            levelId: 0,
            destinationId: 0
        };
        command = new CrossPointInterrogateMessageCommand(params);
        command.buildCommand();
    });

    describe('toDisplay', () => {
        it('Should get a Display representation of the command', () => {
            // Arrange

            // Act
            const commandDisplay: DisplayCommand = command.toDisplay();
            console.log(`Command toDisplay\n`, commandDisplay);

            // Assert
            // TODO: Refine a bit the test with a real pattern matching
            expect(commandDisplay.SOM).toBeDefined();
            expect(commandDisplay.DATA).toBeDefined();
            expect(commandDisplay.BTC).toBeDefined();
            expect(commandDisplay.CHK).toBeDefined();
            expect(commandDisplay.SOM).toBeDefined();
        });
    });

    describe('toHexDump', () => {
        it('Should get the hexadecimal representation of the command', () => {
            // Arrange

            // Act
            const commandHexDump: string = command.toHexDump();
            console.log(`Command toHexDump\n`, commandHexDump);

            // Assert
            // TODO: Refine a bit the test with a real pattern matching
            expect(commandHexDump.includes('<HexBuffer ')).toEqual(true);
        });
    });

    describe('toJson', () => {
        it('Should get the Json representation of the command', () => {
            // Arrange

            // Act
            const jsonCommand: string = command.toJson();
            console.log(`Command toJson\n ${jsonCommand}`);

            // Assert
            // TODO: Refine a bit the test with a real pattern matching
            expect(jsonCommand.includes('SOM')).toEqual(true);
            expect(jsonCommand.includes('DATA')).toEqual(true);
            expect(jsonCommand.includes('BTC')).toEqual(true);
            expect(jsonCommand.includes('CHK')).toEqual(true);
            expect(jsonCommand.includes('EOM')).toEqual(true);
        });
    });

    describe('toLogDescription', () => {
        it('Should get the Log representation of the command', () => {
            // Arrange

            // Act
            const commandLogRepresentation = command.toLogDescription();
            console.log(`Command toLogDescription\n`, commandLogRepresentation);

            // Assert
            // TODO: Refine a bit the test with a real pattern matching
            expect(commandLogRepresentation).toBeDefined();
        });
    });
});
