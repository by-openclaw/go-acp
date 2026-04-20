import { CrossPointConnectOnGoSalvoGroupMessageCommandParams } from '../../../../src/command/rx/120-crosspoint-connect-on-go-group-salvo-message/params';
import { CommandParamsValidator } from '../../../../src/command/rx/120-crosspoint-connect-on-go-group-salvo-message/params.validator';
import { BootstrapService } from '../../../../src/bootstrap.service';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';
import { LocaleData } from '../../../../src/common/locale-data/locale-data.model';
import { CrossPointConnectOnGoSalvoGroupMessageCommandItems } from '../../../../src/command/rx/120-crosspoint-connect-on-go-group-salvo-message/items';

describe('Crosspoint Connect On Go Group Salvo Message - CommandParamsValidator', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the validator', () => {
            // Arrange
            // generate an array of CrossPointConnectOnGoSalvoGroupMessageCommandItems
            const buildDataArray = new Array<CrossPointConnectOnGoSalvoGroupMessageCommandItems>();

            for (let itemIndex = 880; itemIndex < 1024; itemIndex++) {
                // Add the CrossPoint Connect On Go Salvo Group Message Command Items buffer to the array
                // {matrixId: value, levelId: value, destinationId: value, sourceId: value, salvoId: value}
                buildDataArray.push({
                    matrixId: 0,
                    levelId: 0,
                    destinationId: itemIndex,
                    sourceId: itemIndex,
                    salvoId: 0
                });
            }

            const params: CrossPointConnectOnGoSalvoGroupMessageCommandParams = {
                salvoGroupMessageCommandItems: buildDataArray
            };
            // Act
            const validator = new CommandParamsValidator(params);

            // Assert
            expect(validator).toBeDefined();
            expect(validator.data).toBe(params);
        });
    });

    describe('validate', () => {
        it('Should succeed with valid params', () => {
            // Arrange
            // generate an array of CrossPointConnectOnGoSalvoGroupMessageCommandItems
            const buildDataArray = new Array<CrossPointConnectOnGoSalvoGroupMessageCommandItems>();

            // Add the CrossPoint Connect On Go Salvo Group Message Command Items buffer to the array
            // {matrixId: value, levelId: value, destinationId: value, sourceId: value, salvoId: value}
            buildDataArray.push({
                matrixId: 0,
                levelId: 0,
                destinationId: 895,
                sourceId: 1023,
                salvoId: 16
            });

            const params: CrossPointConnectOnGoSalvoGroupMessageCommandParams = {
                salvoGroupMessageCommandItems: buildDataArray
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors).toBeDefined();
            expect(Object.keys(errors).length).toBe(0);
        });

        it('Should return errors id params are out of range < MIN', () => {
            // Arrange
            // generate an array of CrossPointConnectOnGoSalvoGroupMessageCommandItems
            const buildDataArray = new Array<CrossPointConnectOnGoSalvoGroupMessageCommandItems>();

            // Add the CrossPoint Connect On Go Salvo Group Message Command Items buffer to the array
            // {matrixId: value, levelId: value, destinationId: value, sourceId: value, salvoId: value}
            buildDataArray.push({
                matrixId: -1,
                levelId: -1,
                destinationId: -1,
                sourceId: -1,
                salvoId: -1
            });

            const params: CrossPointConnectOnGoSalvoGroupMessageCommandParams = {
                salvoGroupMessageCommandItems: buildDataArray
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            // expect(errors.matrixId).toBeDefined();
            // expect(errors.matrixId.id).toBe(CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG);
            // expect(errors.levelId).toBeDefined();
            // expect(errors.levelId.id).toBe(CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG);
            // expect(errors.destinationId).toBeDefined();
            // expect(errors.destinationId.id).toBe(CommandErrorsKeys.DESTINATION_IS_OUT_OF_RANGE_ERROR_MSG);
            // expect(errors.sourceId).toBeDefined();
            // expect(errors.sourceId.id).toBe(CommandErrorsKeys.SOURCE_IS_OUT_OF_RANGE_ERROR_MSG);
            // expect(errors.salvoId).toBeDefined();
            // expect(errors.salvoId.id).toBe(CommandErrorsKeys.SALVO_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.salvoGroupMessageCommand).toBeDefined();
            expect(errors.salvoGroupMessageCommand.id).toBe(CommandErrorsKeys.SALVO_GROUP_MESSAGE_COMMAND_IS_OUT_OF_RANGE_ERROR_MSG);
        });

        it('Should return errors if matrixId params are out of range > MAX', () => {
            // Arrange
            // generate an array of CrossPointConnectOnGoSalvoGroupMessageCommandItems
            const buildDataArray = new Array<CrossPointConnectOnGoSalvoGroupMessageCommandItems>();

            // Add the CrossPoint Connect On Go Salvo Group Message Command Items buffer to the array
            // {matrixId: value, levelId: value, destinationId: value, sourceId: value, salvoId: value}
            buildDataArray.push({
                matrixId: 256,
                levelId: 256,
                destinationId: 65536,
                sourceId: 65536,
                salvoId: 128
            });

            const params: CrossPointConnectOnGoSalvoGroupMessageCommandParams = {
                salvoGroupMessageCommandItems: buildDataArray
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            // expect(errors.matrixId).toBeDefined();
            // expect(errors.matrixId.id).toBe(CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG);
            // expect(errors.levelId).toBeDefined();
            // expect(errors.levelId.id).toBe(CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG);
            // expect(errors.destinationId).toBeDefined();
            // expect(errors.destinationId.id).toBe(CommandErrorsKeys.DESTINATION_IS_OUT_OF_RANGE_ERROR_MSG);
            // expect(errors.sourceId).toBeDefined();
            // expect(errors.sourceId.id).toBe(CommandErrorsKeys.SOURCE_IS_OUT_OF_RANGE_ERROR_MSG);
            // expect(errors.salvoId).toBeDefined();
            // expect(errors.salvoId.id).toBe(CommandErrorsKeys.SALVO_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.salvoGroupMessageCommand).toBeDefined();
            expect(errors.salvoGroupMessageCommand.id).toBe(CommandErrorsKeys.SALVO_GROUP_MESSAGE_COMMAND_IS_OUT_OF_RANGE_ERROR_MSG);
        });
        it('Should return errors if levelId params are out of range > MAX', () => {
            // Arrange
            // generate an array of CrossPointConnectOnGoSalvoGroupMessageCommandItems
            const buildDataArray = new Array<CrossPointConnectOnGoSalvoGroupMessageCommandItems>();

            // Add the CrossPoint Connect On Go Salvo Group Message Command Items buffer to the array
            // {matrixId: value, levelId: value, destinationId: value, sourceId: value, salvoId: value}
            buildDataArray.push({
                matrixId: 0,
                levelId: 256,
                destinationId: 0,
                sourceId: 0,
                salvoId: 0
            });

            const params: CrossPointConnectOnGoSalvoGroupMessageCommandParams = {
                salvoGroupMessageCommandItems: buildDataArray
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            // expect(errors.matrixId).toBeDefined();
            // expect(errors.matrixId.id).toBe(CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG);
            // expect(errors.levelId).toBeDefined();
            // expect(errors.levelId.id).toBe(CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG);
            // expect(errors.destinationId).toBeDefined();
            // expect(errors.destinationId.id).toBe(CommandErrorsKeys.DESTINATION_IS_OUT_OF_RANGE_ERROR_MSG);
            // expect(errors.sourceId).toBeDefined();
            // expect(errors.sourceId.id).toBe(CommandErrorsKeys.SOURCE_IS_OUT_OF_RANGE_ERROR_MSG);
            // expect(errors.salvoId).toBeDefined();
            // expect(errors.salvoId.id).toBe(CommandErrorsKeys.SALVO_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.salvoGroupMessageCommand).toBeDefined();
            expect(errors.salvoGroupMessageCommand.id).toBe(CommandErrorsKeys.SALVO_GROUP_MESSAGE_COMMAND_IS_OUT_OF_RANGE_ERROR_MSG);
        });
        it('Should return errors if destinationId params are out of range > MAX', () => {
            // Arrange
            // generate an array of CrossPointConnectOnGoSalvoGroupMessageCommandItems
            const buildDataArray = new Array<CrossPointConnectOnGoSalvoGroupMessageCommandItems>();

            // Add the CrossPoint Connect On Go Salvo Group Message Command Items buffer to the array
            // {matrixId: value, levelId: value, destinationId: value, sourceId: value, salvoId: value}
            buildDataArray.push({
                matrixId: 0,
                levelId: 0,
                destinationId: 65536,
                sourceId: 0,
                salvoId: 0
            });

            const params: CrossPointConnectOnGoSalvoGroupMessageCommandParams = {
                salvoGroupMessageCommandItems: buildDataArray
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            // expect(errors.matrixId).toBeDefined();
            // expect(errors.matrixId.id).toBe(CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG);
            // expect(errors.levelId).toBeDefined();
            // expect(errors.levelId.id).toBe(CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG);
            // expect(errors.destinationId).toBeDefined();
            // expect(errors.destinationId.id).toBe(CommandErrorsKeys.DESTINATION_IS_OUT_OF_RANGE_ERROR_MSG);
            // expect(errors.sourceId).toBeDefined();
            // expect(errors.sourceId.id).toBe(CommandErrorsKeys.SOURCE_IS_OUT_OF_RANGE_ERROR_MSG);
            // expect(errors.salvoId).toBeDefined();
            // expect(errors.salvoId.id).toBe(CommandErrorsKeys.SALVO_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.salvoGroupMessageCommand).toBeDefined();
            expect(errors.salvoGroupMessageCommand.id).toBe(CommandErrorsKeys.SALVO_GROUP_MESSAGE_COMMAND_IS_OUT_OF_RANGE_ERROR_MSG);
        });
        it('Should return errors if sourceId params are out of range > MAX', () => {
            // Arrange
            // generate an array of CrossPointConnectOnGoSalvoGroupMessageCommandItems
            const buildDataArray = new Array<CrossPointConnectOnGoSalvoGroupMessageCommandItems>();

            // Add the CrossPoint Connect On Go Salvo Group Message Command Items buffer to the array
            // {matrixId: value, levelId: value, destinationId: value, sourceId: value, salvoId: value}
            buildDataArray.push({
                matrixId: 0,
                levelId: 0,
                destinationId: 0,
                sourceId: 65536,
                salvoId: 0
            });

            const params: CrossPointConnectOnGoSalvoGroupMessageCommandParams = {
                salvoGroupMessageCommandItems: buildDataArray
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            // expect(errors.matrixId).toBeDefined();
            // expect(errors.matrixId.id).toBe(CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG);
            // expect(errors.levelId).toBeDefined();
            // expect(errors.levelId.id).toBe(CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG);
            // expect(errors.destinationId).toBeDefined();
            // expect(errors.destinationId.id).toBe(CommandErrorsKeys.DESTINATION_IS_OUT_OF_RANGE_ERROR_MSG);
            // expect(errors.sourceId).toBeDefined();
            // expect(errors.sourceId.id).toBe(CommandErrorsKeys.SOURCE_IS_OUT_OF_RANGE_ERROR_MSG);
            // expect(errors.salvoId).toBeDefined();
            // expect(errors.salvoId.id).toBe(CommandErrorsKeys.SALVO_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.salvoGroupMessageCommand).toBeDefined();
            expect(errors.salvoGroupMessageCommand.id).toBe(CommandErrorsKeys.SALVO_GROUP_MESSAGE_COMMAND_IS_OUT_OF_RANGE_ERROR_MSG);
        });
        it('Should return errors if salvoId params are out of range > MAX', () => {
            // Arrange
            // generate an array of CrossPointConnectOnGoSalvoGroupMessageCommandItems
            const buildDataArray = new Array<CrossPointConnectOnGoSalvoGroupMessageCommandItems>();

            // Add the CrossPoint Connect On Go Salvo Group Message Command Items buffer to the array
            // {matrixId: value, levelId: value, destinationId: value, sourceId: value, salvoId: value}
            buildDataArray.push({
                matrixId: 0,
                levelId: 0,
                destinationId: 0,
                sourceId: 0,
                salvoId: 128
            });

            const params: CrossPointConnectOnGoSalvoGroupMessageCommandParams = {
                salvoGroupMessageCommandItems: buildDataArray
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            // expect(errors.matrixId).toBeDefined();
            // expect(errors.matrixId.id).toBe(CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG);
            // expect(errors.levelId).toBeDefined();
            // expect(errors.levelId.id).toBe(CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG);
            // expect(errors.destinationId).toBeDefined();
            // expect(errors.destinationId.id).toBe(CommandErrorsKeys.DESTINATION_IS_OUT_OF_RANGE_ERROR_MSG);
            // expect(errors.sourceId).toBeDefined();
            // expect(errors.sourceId.id).toBe(CommandErrorsKeys.SOURCE_IS_OUT_OF_RANGE_ERROR_MSG);
            // expect(errors.salvoId).toBeDefined();
            // expect(errors.salvoId.id).toBe(CommandErrorsKeys.SALVO_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.salvoGroupMessageCommand).toBeDefined();
            expect(errors.salvoGroupMessageCommand.id).toBe(CommandErrorsKeys.SALVO_GROUP_MESSAGE_COMMAND_IS_OUT_OF_RANGE_ERROR_MSG);
        });
        it('Should return errors if empty params are out of range > MAX', () => {
            // Arrange
            // generate an array of CrossPointConnectOnGoSalvoGroupMessageCommandItems
            const buildDataArray = new Array<CrossPointConnectOnGoSalvoGroupMessageCommandItems>();

            const params: CrossPointConnectOnGoSalvoGroupMessageCommandParams = {
                salvoGroupMessageCommandItems: buildDataArray
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            // expect(errors.matrixId).toBeDefined();
            // expect(errors.matrixId.id).toBe(CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG);
            // expect(errors.levelId).toBeDefined();
            // expect(errors.levelId.id).toBe(CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG);
            // expect(errors.destinationId).toBeDefined();
            // expect(errors.destinationId.id).toBe(CommandErrorsKeys.DESTINATION_IS_OUT_OF_RANGE_ERROR_MSG);
            // expect(errors.sourceId).toBeDefined();
            // expect(errors.sourceId.id).toBe(CommandErrorsKeys.SOURCE_IS_OUT_OF_RANGE_ERROR_MSG);
            // expect(errors.salvoId).toBeDefined();
            // expect(errors.salvoId.id).toBe(CommandErrorsKeys.SALVO_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.salvoGroupMessageCommand).toBeDefined();
            expect(errors.salvoGroupMessageCommand.id).toBe(CommandErrorsKeys.SALVO_GROUP_MESSAGE_COMMAND_IS_OUT_OF_RANGE_ERROR_MSG);
        });
    });
});
